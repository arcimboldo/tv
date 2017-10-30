package main

import (
	"flag"
	"fmt"
	"log"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/arcimboldo/tv/eztv"
	tms "github.com/arcimboldo/tv/transmission"
)

var (
	flagN      = flag.Int("n", 5, "Number of shows to return")
	flagT      = flag.String("t", ".*", "Matching title")
	flagS      = flag.String("s", "any", "Matching season")
	flagE      = flag.String("e", "any", "Matching episode")
	flagMNum   = flag.Int("m", 1, "How many matches.")
	flagList   = flag.Bool("list", false, "List last N shows")
	flagSearch = flag.Bool("search", false, "Search for shows")
	flagAdd    = flag.Bool("add", false, "Add to torrent")
	flagLong   = flag.Bool("l", false, "Long listing")
)

func reMatching(r *regexp.Regexp) func(eztv.Show) bool {
	return func(s eztv.Show) bool { return r.MatchString(s.Title) }
}

func seasonMatching(season string) func(eztv.Show) bool {
	if season == "any" {
		return func(s eztv.Show) bool { return true }
	}
	sint, err := strconv.Atoi(season)
	if err != nil {
		panic(err)
	}
	return func(s eztv.Show) bool { return s.Season == sint }
}

func episodeMatching(e string) func(eztv.Show) bool {
	if e == "any" {
		return func(s eztv.Show) bool { return true }
	}
	eint, err := strconv.Atoi(e)
	if err != nil {
		panic(err)
	}
	return func(s eztv.Show) bool { return s.Episode == eint }
}

func matching() []eztv.Show {
	re := reMatching(regexp.MustCompile(*flagT))
	season := seasonMatching(*flagS)
	episode := episodeMatching(*flagE)
	f := func(s eztv.Show) bool {
		return episode(s) && season(s) && re(s)
	}

	shows, err := eztv.LastMatchingN(*flagMNum, f)
	if err != nil {
		log.Printf("error while getting shows: %v", err)
	}
	return shows
}

func listShows(n int) []eztv.Show {
	shows, err := eztv.LatestShows(n)
	if err != nil {
		panic(err)
	}

	return shows
}

func getPathFromShow(s eztv.Show, basepath string) string {
	re := regexp.MustCompile("(.*)S([0-9]*)E([0-9]*).*")
	m := re.FindStringSubmatch(s.Filename)
	if m == nil {
		return filepath.Join(basepath, "incoming")
	}
	return filepath.Join(basepath, strings.Trim(m[1], "."), m[2])
}

func main() {
	flag.Parse()
	shows := []eztv.Show{}
	if *flagList {
		shows = listShows(*flagN)
	} else if *flagSearch {
		shows = matching()
	} else if *flagAdd {
		t, err := tms.NewClient("http://localhost:9091", "admin", "admin")
		if err != nil {
			panic(err)
		}
		shows = matching()
		for _, s := range shows {
			path := getPathFromShow(s, "/datadisk/anmess/Videos/series")
			tinfo, e := t.AddTorrentTo(s.MagnetURL, path)
			if e != nil {
				log.Printf("ERROR: adding show %s: %v", s.Title, e)
			} else {
				log.Printf("Added show %q - id %d, downloading in %q", s.Title, tinfo.ID, path)
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
