package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/arcimboldo/tv/eztv"
	"github.com/arcimboldo/tv/transmission"

	"gopkg.in/yaml.v2"
)

var (
	// commands
	flagUpdateAll = flag.Bool("update-all", false, "Update all known shows")
	flagList      = flag.String("list", "", "List shows. Can be \"local\" or \"all\"")
	flagShow      = flag.String("show", "", "Show show 'show'")
	// options for -show
	flagUpdate = flag.Bool("update", false, "Update show - requires -show")
	flagAdd    = flag.String("add", "", "Add the show - requires URL")
	flagAll    = flag.Bool("all", false, "Update all episodes, not just the newest ones")
	flagLong   = flag.Bool("l", false, "Long listing")
	// generic options
	flagQuiet = flag.Bool("q", false, "quieter output")
	flagF     = flag.String("f", "~/.eztvupdate.yaml", "Configuration file")
	dryRun    = flag.Bool("dry-run", false, "Do not actually update")
)

type Config struct {
	Transmission TrCfg     `yaml:"transmission"`
	Data         DataCfg   `yaml:"data"`
	Shows        []ShowCfg `yaml:"shows"`
}

type TrCfg struct {
	URL      string `yaml:"url"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
}

type ShowCfg struct {
	Title string `yaml:"title"`
	URL   string `yaml:"url"`
	Path  string `yaml:"path"`
}

type DataCfg struct {
	DefaultPath string `yaml:"default_path"`
}

type DownloadedEpisode struct {
	Title   string
	Season  int
	Episode int
	Path    string
}

func expandUser(path string) string {
	if path[:2] == "~/" {
		usr, _ := user.Current()
		dir := usr.HomeDir
		path = filepath.Join(dir, path[2:])
	}
	return path
}

func defaultConfig() Config {
	return Config{
		Transmission: TrCfg{URL: "http://localhost:9091", User: "admin"},
		Data:         DataCfg{DefaultPath: expandUser("~/eztv")},
	}
}

func ConfigFromFile(fname string) (Config, error) {
	cfg := defaultConfig()

	data, err := ioutil.ReadFile(fname)
	if err != nil {
		if !os.IsNotExist(err) {
			return cfg, fmt.Errorf("error while reading file %q: %v", fname, err)
		}
	}

	err = yaml.Unmarshal(data, &cfg)

	return cfg, err
}

func SaveConfig(cfg Config, fname string) error {
	if *dryRun {
		log.Println("SaveConfig: not writing file because --dry-run was used")
		return nil
	}
	// Ensure we don't have duplicates - it's stupid but it happens
	urls := make(map[string]ShowCfg)

	for _, s := range cfg.Shows {
		if _, ok := urls[s.URL]; !ok {
			urls[s.URL] = s
		} else {
			log.Printf("Warning: duplicate entry %s", s.URL)
		}
	}
	cfg.Shows = []ShowCfg{}
	for _, s := range urls {
		cfg.Shows = append(cfg.Shows, s)
	}

	sort.Slice(cfg.Shows, func(i, j int) bool { return cfg.Shows[i].Title < cfg.Shows[j].Title })

	mode := os.FileMode(0644)
	fi, err := os.Stat(fname)
	if err == nil {
		mode = fi.Mode()
	}

	out, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	// if file exist, get mode from it
	return ioutil.WriteFile(fname, out, mode)
}

func getShow(s string, cfg Config) (eztv.Show, bool, error) {
	// Search local show
	found := 0
	var match eztv.Show
	for _, show := range cfg.Shows {
		if *flagShow == show.Title || *flagShow == show.URL {
			eztvShow, err := eztv.GetShow(show.URL)
			if err != nil {
				return eztvShow, false, err
			}
			match = eztvShow
			found += 1
		}
	}
	if found > 0 {
		if found > 1 {
			return match, true, fmt.Errorf("multiple matches for show %q (%d)", s, found)
		}
		return match, true, nil
	}

	shows, err := eztv.ListShows()
	if err != nil {
		return eztv.Show{}, false, err
	}
	r := regexp.MustCompile(fmt.Sprintf("(?i)%s", s))

	found = 0
	for _, show := range shows {
		if s == show.Title || s == show.URL || r.MatchString(show.Title) {
			match, err = eztv.GetShow(show.URL)
			if err != nil {
				log.Printf("error while getting show %s: %v", show.URL, err)
			}
			found++
		}
	}
	if found > 0 {
		if found > 1 {
			return match, false, fmt.Errorf("multiple matches for show %q (%d)", s, found)
		}
		return match, false, nil
	}

	return eztv.Show{}, false, fmt.Errorf("no such show with title or url %q", s)

}

func updateShow(show eztv.Show, cfg Config, all bool) error {
	var t *transmission.Transmission
	var err error
	if !*dryRun {
		t, err = transmission.NewClient(cfg.Transmission.URL, cfg.Transmission.User, cfg.Transmission.Password)
	}
	if err != nil {
		return err
	}
	downloaded := show.GetDownloadedEpisodes(cfg.Data.DefaultPath)
	if err != nil {
		return fmt.Errorf("unable to get list of existing episodes: %v", err)
	}

	latest := show.LatestEpisode()

	toAdd := make(map[int]map[int][]eztv.Episode)
	for _, e := range show.Episodes {

		if e.Downloaded {
			continue
		}
		if !all && !(e.Season >= latest.Season && e.Episode >= latest.Episode) {
			continue
		}
		if _, ok := downloaded[e.Season]; ok {
			if _, ok := downloaded[e.Season][e.Episode]; ok {
				continue
			}
		}

		if _, ok := toAdd[e.Season]; !ok {
			toAdd[e.Season] = make(map[int][]eztv.Episode)
		}

		toAdd[e.Season][e.Episode] = append(toAdd[e.Season][e.Episode], *e)
	}

	for s := range toAdd {
		for e := range toAdd[s] {
			var bestMatch eztv.Episode
			for _, ep := range toAdd[s][e] {
				if bestMatch.MagnetURL == "" {
					bestMatch = ep
				} else if strings.Contains(ep.Title, "1080p") {
					bestMatch = ep
				} else if strings.Contains(ep.Title, "720p") {
					bestMatch = ep
				}
			}

			path := filepath.Join(cfg.Data.DefaultPath, show.Title, fmt.Sprintf("S%02d", bestMatch.Season))
			if *dryRun {
				log.Printf("dry-run: adding episode %s to %s\n", bestMatch, path)
			} else {
				tinfo, err := t.AddTorrentTo(bestMatch.MagnetURL, path)
				if err != nil {
					fmt.Printf("ERROR: adding show %s: %v\n", bestMatch, err)
				} else {
					fmt.Printf("Added show %q S%02dE%02d - id %d, downloading in %q\n", bestMatch.ShowTitle, bestMatch.Season, bestMatch.Episode, tinfo.ID, path)
				}
			}
		}
	}
	return nil
}

func main() {
	flag.Parse()
	fname := expandUser(*flagF)

	cfg, err := ConfigFromFile(fname)
	if err != nil {
		log.Fatalf("Error while parsing configuration file %q: %v", *flagF, err)
	}

	// Check mutually exclusive options
	cmds := 0
	if *flagList != "" {
		cmds++
	}
	if *flagShow != "" {
		cmds++
	}
	if *flagUpdateAll {
		cmds++
	}
	if cmds != 1 {
		log.Fatalf("Exactly one of -update-all, -list -show, add options must be given")
	}

	if *flagList != "" {
		if *flagList == "local" || *flagList == "all" {
			if len(cfg.Shows) == 0 {
				fmt.Println("No shows saved")
			}
			for _, show := range cfg.Shows {
				fmt.Printf("l %-40s %s\n", show.Title, show.URL)
			}
			if *flagList == "all" {
				shows, err := eztv.ListShows()
				if err != nil {
					log.Fatal(err)
				}
				for _, show := range shows {
					fmt.Printf("r %-40s %s\n", show.Title, show.URL)

				}
			}
		} else {
			log.Fatalf("Invalid value for option -list: %q. Must be either \"local\" or \"all\"", *flagList)
		}
	}

	// Show show or update show
	if *flagShow != "" {
		// is in the config?
		show, local, err := getShow(*flagShow, cfg)
		if err != nil {
			log.Fatalf("Error while getting show %q: %v", *flagShow, err)
		}
		if !*flagQuiet {
			fmt.Println(show)
		}

		downloaded := show.GetDownloadedEpisodes(cfg.Data.DefaultPath)
		for _, e := range show.Episodes {
			if _, ok := downloaded[e.Season]; ok {
				if _, ok := downloaded[e.Season][e.Episode]; ok {
					if !*flagQuiet {
						if e.Downloaded {
							fmt.Printf("d %s - %s\n", e, e.FullPath(cfg.Data.DefaultPath))
						} else {
							fmt.Printf("+ %s - %s\n", e, e.TorrentURL)
						}
					}
					continue
				}
			}
			if !*flagQuiet {
				fmt.Printf("  %s - %s\n", e, e.TorrentURL)
			}
		}
		if *flagUpdate {
			if !local {
				cfg.Shows = append(cfg.Shows, ShowCfg{Title: show.Title, URL: show.URL})
			}
			defer SaveConfig(cfg, fname)

			err = updateShow(show, cfg, *flagAll)
			if err != nil {
				log.Fatalf("Error while updating show %s: %v", show.Title, err)
			}
		}
		if *flagAdd != "" {
			show, _, err := getShow(*flagShow, cfg)
			if err != nil {
				log.Fatalf("Error while getting show %q: %v", *flagShow, err)
			}
			if !*flagQuiet {
				fmt.Println(show)
			}

			var e *eztv.Episode
			for _, ep := range show.Episodes {
				if ep.TorrentURL == *flagAdd {
					e = ep
					break
				}

			}
			if e == nil {
				log.Fatalf("Torrent %s not found for show %s", *flagAdd, show)
			}
			if !*dryRun {
				path := filepath.Join(cfg.Data.DefaultPath, show.Title, fmt.Sprintf("S%02d", e.Season))
				t, err := transmission.NewClient(cfg.Transmission.URL, cfg.Transmission.User, cfg.Transmission.Password)
				tinfo, err := t.AddTorrentTo(e.MagnetURL, path)
				if err != nil {
					fmt.Printf("ERROR: adding show %s: %v\n", e, err)
				} else {
					fmt.Printf("Added show %q S%02dE%02d - id %d, downloading in %q\n", e.ShowTitle, e.Season, e.Episode, tinfo.ID, path)
				}
			}
		}
	}

	if *flagUpdateAll {
		for _, s := range cfg.Shows {
			if !*flagQuiet {
				log.Printf("getting show %s (%s)", s.Title, s.URL)
			}
			show, _, err := getShow(s.URL, cfg)
			err = updateShow(show, cfg, *flagAll)
			if err != nil {
				log.Printf("Error while updating show %s: %v", show.Title, err)
			}
		}
	}
}
