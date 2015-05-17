package main

import (
	"database/sql"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/inominate/apicache"

	"github.com/boltdb/bolt"
	"github.com/codegangsta/martini"
	_ "github.com/go-sql-driver/mysql"
	"github.com/inominate/session"
	"github.com/robfig/config"
)

var sesManager *session.SessionManager
var conf *config.Config
var exemptRoles []int64
var exemptChars []string

func setupMartini() *martini.ClassicMartini {
	r := martini.NewRouter()
	m := martini.New()
	m.Use(martini.Logger())
	m.Use(martini.Recovery())
	m.Use(SessionService())
	m.MapTo(r, (*martini.Routes)(nil))
	m.Action(r.Handle)
	return &martini.ClassicMartini{m, r}
}

func loadConfig() error {
	newConf, err := config.ReadDefault("purger.conf")
	if err != nil {
		return err
	}
	conf = newConf

	confStr, err := conf.String("purger", "exemptCharacters")
	if err == nil {
		exemptChars = strings.Split(strings.ToLower(confStr), ",")
		for k := range exemptChars {
			exemptChars[k] = strings.TrimSpace(exemptChars[k])
		}
	} else {
		log.Printf("No exempted characters found: %s", err)
		exemptChars = []string{}
	}

	confStr, err = conf.String("purger", "exemptRoles")
	if err == nil {
		exemptRolesStrings := strings.Split(confStr, ",")
		exemptRoles = make([]int64, len(exemptRolesStrings))
		for k, v := range exemptRolesStrings {
			v = strings.TrimSpace(v)
			if v == "" {
				continue
			}

			role, err := strconv.ParseInt(v, 10, 64)
			if err != nil || role == 0 || role < 0 {
				log.Printf("Error parsing role '%s': %s", v, err)
				continue
			}

			exemptRoles[k] = role
		}
	} else {
		log.Printf("No exempted roles found: %s", err)
		exemptRoles = []int64{}
	}

	return nil
}

var bdb *bolt.DB

func main() {
	var err error

	err = loadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %s", err)
	}

	c := conf
	keyid, err := c.Int("purger", "keyid")
	if err != nil {
		log.Fatalf("Failed to load config: %s", err)
	}
	vcode, err := c.String("purger", "vcode")
	if err != nil {
		log.Fatalf("Failed to load config: %s", err)
	}
	days, err := c.Int("purger", "maxIdleDays")
	if err != nil {
		log.Fatalf("Failed to load config: %s", err)
	}
	listen, err := c.String("purger", "listen")
	if err != nil {
		log.Fatalf("Failed to load config: %s", err)
	}
	if listen == "ENV" {
		listen = ":" + os.Getenv("PORT")
	}

	baseURL, _ := c.String("purger", "APIBaseURL")
	if baseURL == "" {
		baseURL = "https://api.eveonline.com/"
	}
	apiClient := apicache.NewClient(apicache.NilCache)
	apiClient.BaseURL = baseURL

	var memberDB *sql.DB
	var query string
	var cmt CorpMemberTracker

	memberDSN, _ := c.String("registered_characters", "DSN")
	memberURL, _ := c.String("registered_characters", "URL")
	if memberDSN != "" {
		memberDB, err = sql.Open("mysql", memberDSN)
		if err != nil {
			log.Fatalf("could not open database: %s", err)
		}

		query, _ = c.String("registered_characters", "all_query")
		if query == "" {
			log.Fatalf("registered_characters dsn specified but no all_query found.")
		}

		query, _ = c.String("registered_characters", "single_query")
		if query == "" {
			log.Fatalf("registered_characters dsn specified but no single_query found.")
		}
		cmt = NewSQLCorpMemberTracker(memberDB)
	} else if memberURL != "" {
		cmt = NewHTTPCorpMemberTracker(memberURL)
	}

	var store session.SessionStorage

	dbfile, _ := c.String("purger", "boltDB")
	if dbfile == "" {
		log.Fatalf("Need a boltDB file specified for the bolt database.")
	}

	bdb, err = bolt.Open("purger.db", 0644, &bolt.Options{Timeout: 100 * time.Millisecond})
	if err != nil {
		log.Fatalf("Failed to open db: %s", err)
	}
	store, _ = session.NewBoltStore(bdb, time.Hour*24)
	sesManager, _ = session.NewSessionManager(store, "slopemaker_session")

	loadState()

	m := setupMartini()

	m.Get("/", forceLogin, handleRoot)
	m.Get("/purger", forceLogin, handleRoot)

	m.Get("/strip", forceLogin, handleStrip)
	m.Post("/strip", forceLogin, handleStrip)

	m.Get("/boot", forceLogin, handleBoot)
	m.Post("/boot", forceLogin, handleBoot)

	m.Get("/stats", forceLogin, handleStats)

	m.Get("/login", displayLogin)
	m.Post("/login", handleLogin)

	m.Use(martini.Static("static", martini.StaticOptions{Prefix: "static/"}))

	go membersUpdater(apiClient, int64(keyid), vcode, time.Duration(days)*time.Hour*24, cmt)
	go http.ListenAndServe(listen, m)

	sch := make(chan os.Signal, 1)
	signal.Notify(sch, syscall.SIGHUP)
	for _ = range sch {
		saveState()
		err = loadConfig()
		if err != nil {
			log.Printf("Failed to reload config: %s", err)
			continue
		}
		log.Printf("Reloaded user configuration.")
	}
}
