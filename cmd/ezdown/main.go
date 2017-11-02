package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/arcimboldo/tv/eztv"
	tms "github.com/arcimboldo/tv/transmission"
)

var (
	flagN       = flag.Int("n", 5, "Number of shows to return")
	flagT       = flag.String("t", ".*", "Matching title")
	flagS       = flag.String("s", "any", "Matching season")
	flagE       = flag.String("e", "any", "Matching episode")
	flagMNum    = flag.Int("m", 1, "How many matches.")
	flagList    = flag.Bool("list", false, "List last N shows")
	flagSearch  = flag.Bool("search", false, "Search for shows")
	flagAdd     = flag.Int("add", -1, "Add to torrent")
	flagShow    = flag.String("show", "", "show")
	flagGetShow = flag.String("get", "", "show")
	flagLong    = flag.Bool("l", false, "Long listing")
	flagTrPwd   = flag.String("tp", os.Getenv("TRANSMISSION_PASSWORD"), "Password to access transmissino")
)

func reMatching(r *regexp.Regexp) func(eztv.RSSShow) bool {
	return func(s eztv.RSSShow) bool { return r.MatchString(s.Title) }
}

func seasonMatching(season string) func(eztv.RSSShow) bool {
	if season == "any" {
		return func(s eztv.RSSShow) bool { return true }
	}
	sint, err := strconv.Atoi(season)
	if err != nil {
		panic(err)
	}
	return func(s eztv.RSSShow) bool { return s.Season == sint }
}

func episodeMatching(e string) func(eztv.RSSShow) bool {
	if e == "any" {
		return func(s eztv.RSSShow) bool { return true }
	}
	eint, err := strconv.Atoi(e)
	if err != nil {
		panic(err)
	}
	return func(s eztv.RSSShow) bool { return s.Episode == eint }
}

func matching() []eztv.RSSShow {
	re := reMatching(regexp.MustCompile(*flagT))
	season := seasonMatching(*flagS)
	episode := episodeMatching(*flagE)
	f := func(s eztv.RSSShow) bool {
		return episode(s) && season(s) && re(s)
	}

	shows, err := eztv.LastMatchingN(*flagMNum, f)
	if err != nil {
		log.Printf("error while getting shows: %v", err)
	}
	return shows
}

func listShows(n int) []eztv.RSSShow {
	shows, err := eztv.LatestShows(n)
	if err != nil {
		panic(err)
	}

	return shows
}

func getPathFromShow(fname, basepath string) string {
	re := regexp.MustCompile("(.*)S([0-9]*)E([0-9]*).*")
	m := re.FindStringSubmatch(fname)
	if m == nil {
		return filepath.Join(basepath, "incoming")
	}
	return filepath.Join(basepath, strings.Trim(m[1], "."), m[2])
}

func main() {
	flag.Parse()
	shows := []eztv.RSSShow{}
	if *flagList {
		shows = listShows(*flagN)
	} else if *flagSearch {
		shows = matching()
	} else if *flagShow != "" {
		r, err := regexp.Compile(*flagShow)
		if err != nil {
			log.Fatalf("Invalid regexp %q: %v", *flagShow, err)
		}

		shows, err := eztv.ListShows()
		if err != nil {
			log.Fatal(err)
		}
		for _, show := range shows {
			if r.MatchString(show.Title) {
				fmt.Printf("%s - %s\n", show.Title, show.URL)
			}
		}
	} else if *flagGetShow != "" {
		r, err := regexp.Compile(*flagGetShow)
		if err != nil {
			log.Fatalf("Invalid regexp %q: %v", *flagGetShow, err)
		}

		shows, err := eztv.ListShows()
		if err != nil {
			log.Fatal(err)
		}
		for _, show := range shows {
			if r.MatchString(show.Title) {
				show, err := eztv.GetShow(show.URL)
				if err != nil {
					panic(err)
				}
				fmt.Println(show)
				if *flagAdd > -1 {
					t, err := tms.NewClient("http://localhost:9091", "admin", *flagTrPwd)
					if err != nil {
						panic(err)
					}
					episode := show.Episodes[*flagAdd]
					path := getPathFromShow(episode.Filename(), "/datadisk/anmess/Videos/series")
					tinfo, err := t.AddTorrentTo(episode.MagnetURL, path)
					if err != nil {
						fmt.Printf("ERROR: adding show %s: %v", episode, err)
					} else {
						fmt.Printf("Added show %q S%02dE%02d - id %d, downloading in %q", episode.ShowTitle, episode.Season, episode.Episode, tinfo.ID, path)
					}
				} else {
					for i, e := range show.Episodes {
						fmt.Printf("%3d  %s\n", i, e)
					}
				}
			}
		}
	} else {
		log.Fatal("Invalid usage: must be called with either --list or --search")
	}

	for _, show := range shows {
		if *flagLong {
			fmt.Println(show)
			fmt.Println()
		} else {
			fmt.Printf("%s %s - %s\n", show.Released, show.Title, show.EpisodeURL)
		}
	}

}
