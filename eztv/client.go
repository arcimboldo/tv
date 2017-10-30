package eztv

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var (
	eztvURL     string = "https://eztv.ag/api/get-torrents"
	maxPageSize int    = 100
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

type Show struct {
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

func (s Show) String() string {
	msg := fmt.Sprintf(`Title:     %s
Season:    %d
Episode:   %d
Released:  %s
URL:       %s
Filename:  %s`, s.Title, s.Season, s.Episode, s.Released, s.EpisodeURL, s.Filename)
	return msg
}

// LatestShow gets latest n show from EZTV rss
func LatestShows(n int) ([]Show, error) {

	shows := []Show{}

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

func lastShowsPaged(n, p int) ([]Show, error) {
	u := fmt.Sprintf("%s?limit=%d&page=%d", eztvURL, n, p)
	resp, err := http.Get(u)
	if err != nil {
		return []Show{}, err
	}
	defer resp.Body.Close()
	var s struct {
		Torrents []Show `json:"torrents"`
	}
	err = json.NewDecoder(resp.Body).Decode(&s)
	return s.Torrents, err

}

// LastMatching gets the latest show for which the function returns true
func LastMatching(f func(Show) bool) (Show, error) {
	shows, err := LastMatchingN(1, f)
	if len(shows) == 0 {
		return Show{}, err
	}
	return shows[0], err
}

func LastMatchingN(n int, f func(Show) bool) ([]Show, error) {
	shows := []Show{}
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
