package eztv

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

var (
	eztvURL            string = "https://eztv.ag/api/get-torrents"
	maxPageSize        int    = 100
	defaultMovieRegexp string = "(?i)(.*)\\s*S?([0-9]+)[Ex]([0-9]+).*\\.(mkv|avi|mp4|asf|mov|flv|swf|qt|vob|ogg|ogv|yuv|mpg|mpg2|mpeg|mpv|m4v)"
)

type UnixTime struct {
	time.Time
}

func (t *UnixTime) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), "\"")
	if s == "null" {
		t.Time = time.Time{}
		return nil
	}
	timestamp, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		t.Time = time.Time{}
		return nil
	}
	t.Time = time.Unix(timestamp, 0)
	return nil
}

type Episode struct {
	Season     int
	Episode    int
	Quality    string
	Title      string
	EpisodeURL string
	TorrentURL string
	MagnetURL  string
	ShowTitle  string
	ShowURL    string
	Size       string
	Release    string
	Downloaded bool
	Path       string
}

func (e Episode) String() string {
	return fmt.Sprintf("S%02d E%02d - %s - (%s) (%s)", e.Season, e.Episode, e.Title, e.Size, e.Release)
}

func (e Episode) Filename() string {
	base := filepath.Base(e.TorrentURL)
	ext := filepath.Ext(base)
	return base[:len(base)-len(ext)]
}

func (e Episode) FullPath(basedir string) string {
	return filepath.Join(basedir, e.ShowTitle, fmt.Sprintf("S%02d", e.Season), e.Filename())
}

type Show struct {
	Title    string
	URL      string
	Rating   string
	Episodes []*Episode
}

func (s Show) String() string {
	return fmt.Sprintf(`
Title:  %s
URL:    %s
Rating: %s`, s.Title, s.URL, s.Rating)
}

func (s *Show) LatestEpisode() Episode {
	latest := Episode{}
	for _, e := range s.Episodes {
		if e.Downloaded && (e.Season > latest.Season || (e.Season >= latest.Season && e.Episode >= latest.Episode)) {
			latest = *e
		}
	}
	return latest
}

// fuzzyPathMatching matches two paths that *looks* the same
func fuzzyPathMatching(p1, p2 string) bool {
	p1, p2 = strings.ToLower(p1), strings.ToLower(p2)
	if p1 == p2 {
		return true
	}
	e1 := filepath.Ext(p1)
	e2 := filepath.Ext(p2)
	if e1 != e2 {
		// two different types of movie cannot match
		return false
	}
	// Sometimes they have '[eztv]' suffix

	b1 := p1[:len(p1)-len(e1)]
	b2 := p2[:len(p2)-len(e2)]
	if strings.HasSuffix(b1, "[eztv]") {
		if fuzzyPathMatching(b1[:len(b1)-6], b2) {
			return true
		}
	}
	if strings.HasSuffix(b2, "[eztv]") {
		if fuzzyPathMatching(b1, b2[:len(b2)-6]) {
			return true
		}
	}
	return false

}

func (show *Show) GetDownloadedEpisodes(basedir string) map[int]map[int]string {
	downloaded, err := show.getExistingEpisodes(basedir)
	if err != nil {
		return downloaded
	}
	for _, e := range show.Episodes {
		if _, ok := downloaded[e.Season]; ok {
			if path, ok := downloaded[e.Season][e.Episode]; ok {
				// check if this file matches the path
				be := filepath.Base(e.FullPath(basedir))
				bf := filepath.Base(path)

				if fuzzyPathMatching(be, bf) {
					e.Downloaded = true
					e.Path = path
				}
			}
		}
	}
	return downloaded
}

func (show *Show) getExistingEpisodes(basedir string) (map[int]map[int]string, error) {
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
		re := regexp.MustCompile(defaultMovieRegexp)
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

type RSSShow struct {
	ID         int    `json:"id"`
	ImdbID     string `json:"imdb_id"`
	Title      string `json:"title"`
	Season     int    `json:"season,string"`
	Episode    int    `json:"episode,string"`
	Hash       string `json:"hash"`
	Filename   string `json:"filename"`
	EpisodeURL string `json:"episode_url"`
	TorrentURL string `json:"torrent_url"`
	MagnetURL  string `json:"magnet_url"`

	SmallScreenshot string   `json:"small_screenshot"`
	LargeScreenshot string   `json:"large_screenshot"`
	Seeds           int      `json:"seeds"`
	Peers           int      `json:"peers"`
	Released        UnixTime `json:"date_released_unix"`
}

func (s RSSShow) String() string {
	msg := fmt.Sprintf(`Title:     %s
Season:    %d
Episode:   %d
Released:  %s
URL:       %s
Filename:  %s`, s.Title, s.Season, s.Episode, s.Released, s.EpisodeURL, s.Filename)
	return msg
}

// LatestShow gets latest n show from EZTV rss
func LatestShows(n int) ([]RSSShow, error) {

	shows := []RSSShow{}

	if n > maxPageSize {
		var p int
		for p = 1; p*maxPageSize < n; p++ {
			showpage, err := lastShowsPaged(maxPageSize, p)
			if err != nil {
				return shows, err
			}
			shows = append(shows, showpage...)
		}
		remain := n - (p-1)*maxPageSize
		showpage, err := lastShowsPaged(maxPageSize, p+1)
		if err != nil {
			return shows, err
		}
		for i := 0; i < remain; i++ {
			shows = append(shows, showpage[i])
		}
		return shows, nil
	}
	return lastShowsPaged(n, 1)
}

func lastShowsPaged(n, p int) ([]RSSShow, error) {
	u := fmt.Sprintf("%s?limit=%d&page=%d", eztvURL, n, p)
	resp, err := http.Get(u)
	if err != nil {
		return []RSSShow{}, err
	}
	defer resp.Body.Close()
	var s struct {
		Torrents []RSSShow `json:"torrents"`
	}
	err = json.NewDecoder(resp.Body).Decode(&s)
	return s.Torrents, err

}

// LastMatching gets the latest show for which the function returns true
func LastMatching(f func(RSSShow) bool) (RSSShow, error) {
	shows, err := LastMatchingN(1, f)
	if len(shows) == 0 {
		return RSSShow{}, err
	}
	return shows[0], err
}

func LastMatchingN(n int, f func(RSSShow) bool) ([]RSSShow, error) {
	shows := []RSSShow{}
	maxpages := 50
	for p := 1; p <= maxpages && n > 0; p++ {
		pageshows, err := lastShowsPaged(maxPageSize, p)
		if p%10 == 0 {
			log.Printf("Page %d", p)
		}
		if err != nil {
			return shows, err
		}
		for _, show := range pageshows {
			if f(show) {
				shows = append(shows, show)
				n--
				if n == 0 {
					break
				}
			}
		}
	}
	if len(shows) < n {
		return shows, fmt.Errorf("After %d results only %d matching your requests.", maxPageSize*maxpages, len(shows))
	}
	return shows, nil
}

func ListShows() ([]Show, error) {
	var shows []Show
	u, _ := url.Parse("https://eztv.ag/showlist/")
	doc, err := goquery.NewDocument(u.String())
	if err != nil {
		return shows, err
	}

	doc.Find("table tbody tr td.forum_thread_post a").Each(func(i int, s *goquery.Selection) {
		path, _ := s.Attr("href")
		showUrl := u
		showUrl.Path = path
		title := s.Text()

		show := Show{Title: title, URL: showUrl.String()}
		shows = append(shows, show)
	})

	return shows, nil
}

func parseTitle(s string) (title string, season, episode int) {
	re := regexp.MustCompile("(?i)(.*[^\\s]*)\\s*S?([0-9]+)[Ex]([0-9]+).*")
	m := re.FindStringSubmatch(s)
	if m == nil {
		return s, -1, -1
	}
	title = m[1]
	season, _ = strconv.Atoi(m[2])
	episode, _ = strconv.Atoi(m[3])
	return title, season, episode
}

func GetShow(URL string) (Show, error) {
	show := Show{URL: URL}
	doc, err := goquery.NewDocument(URL)
	if err != nil {
		return show, err
	}
	show.Title = doc.Find("td h1 b span").First().Text()
	show.Rating = doc.Find("b span[itemprop=ratingValue]").First().Text()

	doc.Find("table tbody tr").Each(func(i int, sel *goquery.Selection) {
		if sel.Find("td").Size() != 6 {
			return
		}
		if sel.Find("td.forum_thread_post a.epinfo").Size() == 0 {
			return
		}
		// <empty> | title | url | size | released
		title := sel.Find("td.forum_thread_post a.epinfo").Text()
		path, _ := sel.Find("td.forum_thread_post a.epinfo").Attr("href")
		magnet, _ := sel.Find("td.forum_thread_post a.magnet").Attr("href")
		torrent, _ := sel.Find("td.forum_thread_post a.download_1").Attr("href")
		size := sel.Find("td").Eq(3).Text()
		release := sel.Find("td").Eq(4).Text()

		u, _ := url.Parse(URL)
		u.Path = path
		_, s, e := parseTitle(title)
		ep := Episode{
			Title:      title,
			Season:     s,
			Episode:    e,
			MagnetURL:  magnet,
			TorrentURL: torrent,
			EpisodeURL: u.String(),
			ShowTitle:  show.Title,
			ShowURL:    URL,
			Size:       size,
			Release:    release,
		}
		show.Episodes = append(show.Episodes, &ep)
	})

	// Sort episodes
	sort.Slice(show.Episodes, func(i, j int) bool {
		return show.Episodes[i].Season < show.Episodes[j].Season || (show.Episodes[i].Season == show.Episodes[j].Season && show.Episodes[i].Episode < show.Episodes[j].Episode)
	})
	// for i, j := 0, len(show.Episodes)-1; i < j; i, j = i+1, j-1 {
	// 	show.Episodes[i], show.Episodes[j] = show.Episodes[j], show.Episodes[i]
	// }

	return show, nil
}
