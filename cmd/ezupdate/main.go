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
	"strconv"
	"strings"

	"github.com/arcimboldo/tv/eztv"
	"github.com/arcimboldo/tv/transmission"

	"gopkg.in/yaml.v2"
)

var (
	flagF      = flag.String("f", "~/.eztvupdate.yaml", "Configuration file")
	flagUpdate = flag.Bool("update", false, "Update show - need URL")
	flagAll    = flag.Bool("all", false, "Update all episodes, not just the newest ones")
	flagList   = flag.String("list", "", "List shows. Can be \"local\" or \"all\"")
	flagShow   = flag.String("show", "", "Show show 'show'")
	flagQuiet  = flag.Bool("q", false, "quieter output")
	dryRun     = flag.Bool("dry-run", false, "Do not actually update")
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
		return cfg, fmt.Errorf("error while reading file %q: %v", fname, err)
	}

	err = yaml.Unmarshal(data, &cfg)

	return cfg, err
}

type SortableShows []ShowCfg

func (s SortableShows) Len() int           { return len(s) }
func (s SortableShows) Less(i, j int) bool { return s[i].Title < s[j].Title }
func (s SortableShows) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

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

	sort.Sort(SortableShows(cfg.Shows))

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

func getExistingEpisodes(show eztv.Show, basedir string) (map[int]map[int]string, error) {
	files, err := ioutil.ReadDir(basedir)
	var possibleMatches []string
	episodes := make(map[int]map[int]string)
	r := regexp.MustCompile(strings.Replace(show.Title, " ", ".", -1))
	for _, f := range files {
		if f.IsDir() && r.MatchString(f.Name()) {
			possibleMatches = append(possibleMatches, f.Name())
		}
	}
	if len(possibleMatches) > 1 {
		return episodes, fmt.Errorf("too many directories matching show title %q (%d)", show.Title, len(possibleMatches))
	}

	if len(possibleMatches) == 0 {
		return episodes, nil
	}

	walkFn := func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		re := regexp.MustCompile("(.*)[sS]([0-9]*)[eEx]([0-9]*).*")
		if m := re.FindStringSubmatch(filepath.Base(path)); m != nil {
			s, _ := strconv.Atoi(m[2])
			e, _ := strconv.Atoi(m[3])
			if _, ok := episodes[s]; !ok {
				episodes[s] = make(map[int]string)
			}
			episodes[s][e] = path
		}
		return nil
	}

	err = filepath.Walk(filepath.Join(basedir, possibleMatches[0]), walkFn)
	return episodes, err
}

func updateShow(show eztv.Show, cfg Config, all bool) error {
	t, err := transmission.NewClient(cfg.Transmission.URL, cfg.Transmission.User, cfg.Transmission.Password)
	if err != nil {
		return err
	}
	downloaded, err := getExistingEpisodes(show, cfg.Data.DefaultPath)
	if err != nil {
		return fmt.Errorf("unable to get list of existing episodes: %v", err)
	}

	maxSeason, maxEpisode := 0, 0
	for s, _ := range downloaded {
		if s <= maxSeason {
			continue
		}
		maxSeason = s
		maxEpisode = 0
		for e, _ := range downloaded[s] {
			if e > maxEpisode {
				maxEpisode = e
			}
		}
	}

	toAdd := make(map[int]map[int][]eztv.Episode)

	for _, e := range show.Episodes {
		if _, ok := downloaded[e.Season]; ok {
			if _, ok := downloaded[e.Season][e.Episode]; ok {
				continue
			}
		}
		if !all {
			if e.Season < maxSeason {
				continue
			} else if e.Season == maxSeason && e.Episode < maxEpisode {
				continue
			}
		}

		if _, ok := toAdd[e.Season]; !ok {
			toAdd[e.Season] = make(map[int][]eztv.Episode)
		}
		toAdd[e.Season][e.Episode] = append(toAdd[e.Season][e.Episode], e)
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
	} else if *flagShow != "" {
		// is in the config?
		show, _, err := getShow(*flagShow, cfg)
		if err != nil {
			log.Fatalf("Error while getting show %q: %v", *flagShow, err)
		}
		if !*flagQuiet {
			fmt.Println(show)
		}

		downloaded, err := getExistingEpisodes(show, cfg.Data.DefaultPath)
		for _, e := range show.Episodes {
			if _, ok := downloaded[e.Season]; ok {
				if _, ok := downloaded[e.Season][e.Episode]; ok {
					if !*flagQuiet {
						fmt.Printf("d %s\n", e)
					}
					continue
				}
			}
			if !*flagQuiet {
				fmt.Printf("  %s\n", e)
			}
		}
		if *flagUpdate {
			show, local, err := getShow(*flagShow, cfg)
			if err != nil {
				log.Fatalf("Error while getting show %q: %v", *flagShow, err)
			}
			if !local {
				cfg.Shows = append(cfg.Shows, ShowCfg{Title: show.Title, URL: show.URL})
			}
			defer SaveConfig(cfg, fname)

			err = updateShow(show, cfg, *flagAll)
			if err != nil {
				log.Fatalf("Error while updating show %s: %v", show.Title, err)
			}
		}
	}
}
