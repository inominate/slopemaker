package main

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/gob"
	"encoding/xml"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/inominate/apicache"

	"github.com/boltdb/bolt"
)

type purgeMember struct {
	Name      string
	Id        int64
	Joined    time.Time
	LastLogin time.Time
	ShipType  string
	Roles     bool
	Claimed   time.Time
	Stripped  time.Time
	Purged    bool
	Reason    string

	// May god have mercy on my soul.
	IsRegistered func() bool
}

type MemberTrackingMember struct {
	CharacterID    int64   `xml:"characterID,attr"`
	Name           string  `xml:"name,attr"`
	BaseID         int64   `xml:"baseID,attr"`
	Base           string  `xml:"base,attr"`
	Title          string  `xml:"title,attr"`
	StartDateTime  APITime `xml:"startDateTime,attr"`
	LogonDateTime  APITime `xml:"logonDateTime,attr"`
	LogoffDateTime APITime `xml:"logoffDateTime,attr"`
	LocationID     int64   `xml:"locationID,attr"`
	Location       string  `xml:"location,attr"`
	ShipTypeID     int64   `xml:"shipTypeID,attr"`
	ShipType       string  `xml:"shipType,attr"`
	Roles          int64   `xml:"roles,attr"`
	GrantableRoles int64   `xml:"grantableRoles,attr"`
}

func init() {
	gob.Register(purgeMember{})
}

var toBePurged = map[int64]*purgeMember{}
var purgeLock sync.RWMutex

var exemptHulls = []string{"Aeon", "Nyx", "Hel", "Wyvern", "Avatar", "Erebus",
	"Ragnarok", "Leviathan"}

func saveState() {
	purgeLock.Lock()
	defer purgeLock.Unlock()

	err := bdb.Update(func(tx *bolt.Tx) error {
		b, _ := tx.CreateBucketIfNotExists([]byte("purger"))

		buf := &bytes.Buffer{}
		g := gob.NewEncoder(buf)
		err := g.Encode(toBePurged)
		if err != nil {
			return err
		}

		return b.Put([]byte("state"), buf.Bytes())
	})
	if err != nil {
		log.Printf("Failed to save state: %s", err)
	}
}

func loadState() {
	purgeLock.Lock()
	defer purgeLock.Unlock()

	err := bdb.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("purger"))
		if b == nil {
			return errors.New("Bucket does not exist.")
		}

		gobbed := b.Get([]byte("state"))
		if gobbed == nil {
			return errors.New("State not previously saved.")
		}

		buf := bytes.NewBuffer(gobbed)
		g := gob.NewDecoder(buf)
		err := g.Decode(&toBePurged)

		return err
	})
	if err != nil {
		log.Printf("Failed to load state: %s", err)
	}
}

func exempt(m MemberTrackingMember) bool {
	for _, role := range exemptRoles {
		if m.Roles&role == role {
			return true
		}
	}

	lowername := strings.ToLower(m.Name)
	for _, char := range exemptChars {
		if lowername == char {
			return true
		}
	}

	for _, ship := range exemptHulls {
		if m.ShipType == ship {
			return true
		}
	}
	return false
}

type SQLCorpMemberTracker struct {
	db         *sql.DB
	singleStmt *sql.Stmt
	allStmt    *sql.Stmt

	sync.RWMutex
}

func NewSQLCorpMemberTracker(db *sql.DB) *SQLCorpMemberTracker {
	s := SQLCorpMemberTracker{}
	s.db = db

	return &s
}

func (s *SQLCorpMemberTracker) IsRegistered(charName string) bool {
	var err error

	s.Lock()
	defer s.Unlock()

	if s.singleStmt == nil {
		query, _ := conf.String("registered_characters", "single_query")
		if query == "" {
			log.Fatalf("registered_characters dsn specified but no query found.")
		}

		s.singleStmt, err = s.db.Prepare(query)
		if err != nil {
			log.Fatalf("failed to prepare registered character query '%s': %s", query, err)
			return false
		}
	}

	var name string
	err = s.singleStmt.QueryRow(charName).Scan(&name)
	if err != nil {
		if err != sql.ErrNoRows {
			log.Printf("isRegisteredChar error: %s", err)
		}
		return false
	}

	if name == "" {
		log.Printf("got empty name for %s?", charName)
		return false
	}

	if name != charName {
		log.Printf("unexpected charName for isRegisteredChar (%s/%s)", name, charName)
		return false
	}

	return true
}

func (s *SQLCorpMemberTracker) GetMemberMap() (map[string]bool, error) {
	var err error

	s.Lock()
	defer s.Unlock()

	if s.allStmt == nil {
		query, _ := conf.String("registered_characters", "all_query")
		if query == "" {
			log.Fatalf("registered_characters dsn specified but no query found.")
		}

		s.allStmt, err = s.db.Prepare(query)
		if err != nil {
			return nil, fmt.Errorf("failed to prepare registered character query '%s': %s", query, err)
		}
	}

	registeredChars := make(map[string]bool)
	rows, err := s.allStmt.Query()
	if err != nil {
		return nil, fmt.Errorf("failed registered character query: %s", err)
	}

	var charName string
	for rows.Next() {
		err := rows.Scan(&charName)
		if err != nil {
			return nil, fmt.Errorf("failed scanning: %s", err)
		}

		registeredChars[strings.ToLower(charName)] = true
	}

	return registeredChars, nil
}

type CorpMemberTracker interface {
	GetMemberMap() (map[string]bool, error)
	IsRegistered(name string) bool
}

func membersUpdater(apiClient *apicache.Client, keyid int64, vcode string, maxIdle time.Duration, cmt CorpMemberTracker) {
	var registeredChars map[string]bool
	var err error

	memberReq := apiClient.NewRequest("/corp/MemberTracking.xml.aspx")
	memberReq.Set("keyid", fmt.Sprintf("%d", keyid))
	memberReq.Set("vcode", vcode)
	memberReq.Set("extended", "1")

	for {
		if cmt != nil {
			registeredChars, err = cmt.GetMemberMap()
			if err != nil {
				log.Printf("Error getting registered characters: %s", err)
			}
		}

		log.Printf("Pulling current corp member list.")
		resp, err := memberReq.Do()
		if err != nil {
			log.Printf("API Error: %s", err)
			time.Sleep(60 * time.Second)
			continue
		}

		type MemberTracking struct {
			Members []MemberTrackingMember `xml:"result>rowset>row"`
		}

		var members MemberTracking
		err = xml.Unmarshal(resp.Data, &members)
		if err != nil {
			log.Printf("API Error: %s", err)
			time.Sleep(60 * time.Second)
			continue
		}

		purgeLock.Lock()
		var newPurge = map[int64]*purgeMember{}
		var registered bool
		for _, mt := range members.Members {
			if registeredChars == nil {
				registered = true
			} else {
				_, registered = registeredChars[strings.ToLower(mt.Name)]
			}

			if time.Since(mt.LogonDateTime.Time) > maxIdle || !registered {
				if exempt(mt) {
					continue
				}

				var m purgeMember

				m = purgeMember{mt.Name, mt.CharacterID,
					mt.StartDateTime.Time, mt.LogonDateTime.Time,
					mt.ShipType, false, time.Time{}, time.Time{}, false, "", nil}

				if mt.Roles != 0 || mt.GrantableRoles != 0 {
					m.Roles = true
				}

				// Persist strip times and claim times
				if oldm, ok := toBePurged[mt.CharacterID]; ok {
					if !oldm.Stripped.IsZero() && !m.Roles {
						m.Stripped = oldm.Stripped
					}
					if !oldm.Claimed.IsZero() {
						m.Claimed = oldm.Claimed
					}
					if oldm.Roles == true && m.Roles == false {
						m.Stripped = time.Now()
					}
				}

				if time.Since(mt.LogonDateTime.Time) > maxIdle {
					m.Reason += fmt.Sprintf("Idle %s days. ", daysSince(mt.LogonDateTime.Time))
				}
				if !registered {
					m.Reason += "Unregistered."
					regName := m.Name
					m.IsRegistered = func() bool {
						return cmt.IsRegistered(regName)
					}
				}
				if m.Reason == "" {
					log.Printf("I'm supposed to kick %s but I don't know why.\n%#v", m.Name, m)
					continue
				}

				newPurge[mt.CharacterID] = &m
			}
		}

		toBePurged = newPurge
		purgeLock.Unlock()

		go saveState()

		log.Printf("Done. Next pull at %s", resp.Expires.Format(ApiDateTimeFormat))
		select {
		case <-time.After(resp.Expires.Sub(time.Now()) + 30*time.Second):
		}
	}
}

type HTTPCorpMemberTracker struct {
	url         string
	lastUpdate  time.Time
	cachedNames map[string]bool

	sync.RWMutex
}

func NewHTTPCorpMemberTracker(url string) *HTTPCorpMemberTracker {
	var n HTTPCorpMemberTracker
	n.url = url

	n.update()

	return &n
}

func (hcmt *HTTPCorpMemberTracker) update() {
	hcmt.Lock()
	defer hcmt.Unlock()

	if time.Since(hcmt.lastUpdate) < time.Hour {
		return
	}

	resp, err := http.Get(hcmt.url)
	if err != nil {
		log.Printf("Error getting registered member list: %s", err)
		return
	}
	defer resp.Body.Close()

	newNames := map[string]bool{}
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		newNames[strings.ToLower(scanner.Text())] = true
	}
	if err := scanner.Err(); err != nil {
		log.Printf("Error reading registered member list: %s", err)
		return
	}

	hcmt.cachedNames = newNames
	hcmt.lastUpdate = time.Now()
}

func (hcmt *HTTPCorpMemberTracker) GetMemberMap() (map[string]bool, error) {
	hcmt.update()

	hcmt.RLock()
	defer hcmt.RUnlock()

	retMap := map[string]bool{}
	for k, v := range hcmt.cachedNames {
		retMap[k] = v
	}

	return retMap, nil
}

func (hcmt *HTTPCorpMemberTracker) IsRegistered(name string) bool {
	hcmt.update()

	hcmt.RLock()
	defer hcmt.RUnlock()

	_, ok := hcmt.cachedNames[name]
	return ok
}
