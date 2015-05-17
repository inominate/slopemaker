package main

import (
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func formatTime(t time.Time) string {
	return t.Format(ApiDateTimeFormat)
}

func daysSince(t time.Time) string {
	days := time.Since(t) / time.Hour / 24
	return fmt.Sprintf("%d", days)
}

var tFuncMap = template.FuncMap{
	"datetime":  formatTime,
	"dayssince": daysSince,
}

//apparently my sessions are bad and i should feel bad.
func victimsToStr(victims []int64) string {
	var strs []string
	for _, vid := range victims {
		strs = append(strs, fmt.Sprintf("%d", vid))
	}
	return strings.Join(strs, ",")
}

func strToVictims(victimStr string) []int64 {
	var victims []int64

	strs := strings.Split(victimStr, ",")
	for _, vidstr := range strs {
		vid, err := strconv.ParseInt(vidstr, 10, 64)
		if err != nil {
			continue
		}
		victims = append(victims, vid)
	}
	return victims
}

func handleStrip(w http.ResponseWriter, r *http.Request, ses Session) {
	r.ParseForm()

	purgeLock.Lock()
	defer purgeLock.Unlock()
	go saveState()

	victimsStr := ses.Get("strip_victims")
	victims := strToVictims(victimsStr)

	if r.PostFormValue("claim") != "" {
		// Complete the ones marked as completed.  This should be undone if
		// necessary on the next API pull.
		for _, id := range victims {
			var confirmed bool
			if _, ok := toBePurged[id]; !ok {
				continue
			}

			for _, strid := range r.PostForm["stripped"] {
				cid, err := strconv.ParseInt(strid, 10, 64)
				if err != nil {
					continue
				}

				if id == cid {
					confirmed = true
				}
			}

			// mark as purged if confirmation, unclaim it otherwise.
			if confirmed {
				log.Printf("Confirming %s as stripped by %s.", toBePurged[id].Name, ses.Get("username"))
				toBePurged[id].Stripped = time.Now()
			}
			toBePurged[id].Claimed = time.Time{}
		}
		ses.Set("strip_victims", "")
		victims = []int64{}

		if r.PostFormValue("claim") == "complete" {
			w.Header().Set("Location", "strip")
			w.WriteHeader(http.StatusFound)
			return
		}
	}

	if r.PostFormValue("claim") == "victims" {
		var victims []int64
		count := 0
		for id, m := range toBePurged {
			if !m.Roles {
				// Skip anything without roles
				continue
			}

			if time.Since(m.Stripped) <= time.Hour*24 {
				// Skip anything that we think we've stripped
				continue
			}

			if time.Since(m.Claimed) <= time.Hour*1 {
				// If it's been claimed in the last hour, skip it.
				continue
			}

			// IsRegistered is nil for registered characters
			if m.IsRegistered != nil {
				if m.IsRegistered() == true {
					continue
				}
			}

			toBePurged[id].Claimed = time.Now()
			victims = append(victims, id)
			count++
			if count >= 10 {
				break
			}
		}
		ses.Set("strip_victims", victimsToStr(victims))
		w.Header().Set("Location", "strip")
		w.WriteHeader(http.StatusFound)
		return
	}

	stripTemplate, err := template.New("base.html").Funcs(tFuncMap).ParseFiles("templates/base.html", "templates/strip.html")
	if err != nil {
		log.Printf("Template error: %s", err)
		return
	}

	type StripData struct {
		Title   string
		Members []purgeMember
	}
	sd := StripData{Title: "Strip Roles"}

	for _, id := range victims {
		m, ok := toBePurged[id]
		if !ok {
			continue
		}

		if !m.Roles {
			// Skip anything without roles
			continue
		}

		if time.Since(m.Stripped) <= time.Hour*24 {
			// Skip anything that we think we've stripped
			continue
		}

		toBePurged[id].Claimed = time.Now()
		sd.Members = append(sd.Members, *m)
	}

	err = stripTemplate.Execute(w, sd)
	if err != nil {
		log.Printf("Template Error: %s", err)
	}
}

func handleBoot(w http.ResponseWriter, r *http.Request, ses Session) {
	r.ParseForm()

	purgeLock.Lock()
	defer purgeLock.Unlock()
	go saveState()

	victimsStr := ses.Get("boot_victims")
	victims := strToVictims(victimsStr)

	if r.PostFormValue("claim") != "" {
		// Complete the ones marked as completed.  This should be undone if
		// necessary on the next API pull.
		for _, id := range victims {
			var confirmed bool
			if _, ok := toBePurged[id]; !ok {
				continue
			}

			for _, strid := range r.PostForm["kicked"] {
				cid, err := strconv.ParseInt(strid, 10, 64)
				if err != nil {
					continue
				}

				if id == cid {
					confirmed = true
				}
			}

			// mark as purged if confirmation, unclaim it
			if confirmed {
				log.Printf("Confirming %s as purged by %s.", toBePurged[id].Name, ses.Get("username"))
				toBePurged[id].Purged = true
			}
			toBePurged[id].Claimed = time.Time{}
		}
		ses.Set("boot_victims", "")
		victims = []int64{}

		if r.PostFormValue("claim") == "complete" {
			w.Header().Set("Location", "boot")
			w.WriteHeader(http.StatusFound)
			return
		}
	}

	if r.PostFormValue("claim") == "victims" {
		var victims []int64
		count := 0
		for id, m := range toBePurged {
			if m.Roles || m.Purged {
				// Skip anything with roles or that we think we've purged
				continue
			}

			if time.Since(m.Stripped) <= time.Hour*24 {
				// Skip anything that we know is in stasis
				continue
			}

			if time.Since(m.Claimed) <= time.Hour*1 {
				// If it's been claimed in the last hour, skip it.
				continue
			}

			// IsRegistered is nil for registered characters
			if m.IsRegistered != nil {
				if m.IsRegistered() == true {
					continue
				}
			}

			toBePurged[id].Claimed = time.Now()
			victims = append(victims, id)
			count++
			if count >= 10 {
				break
			}
		}
		ses.Set("boot_victims", victimsToStr(victims))
		w.Header().Set("Location", "boot")
		w.WriteHeader(http.StatusFound)
		return
	}

	bootTemplate, err := template.New("base.html").Funcs(tFuncMap).ParseFiles("templates/base.html", "templates/boot.html")
	if err != nil {
		log.Printf("Template error: %s", err)
		return
	}

	type BootData struct {
		Title   string
		Members []purgeMember
	}
	bd := BootData{Title: "Slopes for the Slope Throne"}

	for _, id := range victims {
		m, ok := toBePurged[id]
		if !ok {
			continue
		}

		if m.Roles || m.Purged {
			// Skip anything without roles
			continue
		}

		if time.Since(m.Stripped) <= time.Hour*24 {
			// Skip anything that we think we've stripped
			continue
		}

		toBePurged[id].Claimed = time.Now()
		bd.Members = append(bd.Members, *m)
	}

	err = bootTemplate.Execute(w, bd)
	if err != nil {
		log.Printf("Template Error: %s", err)
	}
}

func handleRoot(w http.ResponseWriter, r *http.Request, ses Session) {
	rootTemplate, err := template.New("base.html").Funcs(tFuncMap).ParseFiles("templates/base.html", "templates/root.html")
	if err != nil {
		log.Printf("Template error: %s", err)
		return
	}
	err = rootTemplate.Execute(w, map[string]string{"Title": "Slope Maker"})
	if err != nil {
		log.Printf("Template Error: %s", err)
	}
}

func handleStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	printStats(w)
}

func printStats(w io.Writer) {
	purgeLock.Lock()
	defer purgeLock.Unlock()

	var totalMembers int
	var needsStripped int
	var needsPurged int
	var inStasis int
	var claimed int

	for _, m := range toBePurged {
		totalMembers++

		if time.Since(m.Claimed) <= 1*time.Hour && time.Since(m.Stripped) > 24*time.Hour {
			claimed++
		}
		if time.Since(m.Stripped) <= 24*time.Hour {
			inStasis++
		} else if m.Roles {
			needsStripped++
		}
		if !m.Roles && time.Since(m.Stripped) > 24*time.Hour {
			needsPurged++
		}
	}
	fmt.Fprintf(w, "Total: %d  Claimed: %d  ToBePurged:  %d\n", totalMembers, claimed, needsPurged)
	fmt.Fprintf(w, "ToBeStripped: %d  InStasis: %d\n\n--------------\n", needsStripped, inStasis)
	for _, m := range toBePurged {
		fmt.Fprintf(w, "%s	%s\n", m.Name, m.Reason)
	}

	log.Printf("Total: %d  Claimed: %d  ToBePurged:  %d", totalMembers, claimed, needsPurged)
	log.Printf("ToBeStripped: %d  InStasis: %d", needsStripped, inStasis)
}
