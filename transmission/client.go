package transmission

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

type Transmission struct {
	URL       string
	sessionId string
	user      string
	pwd       string
}

type TrInfo struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	HashString string `json:"hashString"`
}

func NewClient(URL, user, password string) (*Transmission, error) {
	t := &Transmission{URL: URL, user: user, pwd: password}

	req, err := http.NewRequest("GET", strings.Trim(t.URL, "/")+"/transmission/rpc", nil)
	if err != nil {
		return t, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(t.user, t.pwd)
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return t, err
	}
	defer resp.Body.Close()

	// Get the proper transmission id
	t.sessionId = resp.Header.Get("X-Transmission-Session-Id")
	if t.sessionId == "" {
		return t, fmt.Errorf("unable initialize Transmission client. Server replied %d (%v)", resp.StatusCode, resp.Status)
	}
	return t, nil
}

func (t *Transmission) makeRequest(data io.Reader) (*http.Request, error) {
	req, err := http.NewRequest("POST", fmt.Sprintf("%s/transmission/rpc", t.URL), data)
	if err != nil {
		return req, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Transmission-Session-Id", t.sessionId)
	req.SetBasicAuth(t.user, t.pwd)
	return req, nil
}

func (t *Transmission) AddTorrent(magnet string) (TrInfo, error) {
	data := struct {
		Method    string `json:"method"`
		Arguments struct {
			Filename string `json:"filename"`
		} `json:"arguments"`
	}{}
	data.Method = "torrent-add"
	data.Arguments.Filename = magnet

	b := new(bytes.Buffer)
	json.NewEncoder(b).Encode(data)
	req, err := t.makeRequest(b)
	if err != nil {
		return TrInfo{}, err
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return TrInfo{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return TrInfo{}, fmt.Errorf("got error %d (%s) while adding torrent.", resp.StatusCode, resp.Status)
	}

	jresp := struct {
		Arguments struct {
			Info      TrInfo `json:"torrent-added"`
			Duplicate TrInfo `json:"torrent-duplicate"`
		} `json:"arguments"`
		Result string `json:"result"`
	}{}

	json.NewDecoder(resp.Body).Decode(&jresp)
	if jresp.Result != "success" {
		return TrInfo{}, fmt.Errorf(jresp.Result)
	}
	if jresp.Arguments.Duplicate.HashString != "" {
		return jresp.Arguments.Duplicate, fmt.Errorf("duplicated torrent with id %d", jresp.Arguments.Duplicate.ID)
	}

	return jresp.Arguments.Info, nil

}

func (t *Transmission) AddTorrentTo(magnet, path string) (TrInfo, error) {
	tinfo, err := t.AddTorrent(magnet)
	if err != nil {
		return tinfo, err
	}
	err = os.MkdirAll(path, 0755)
	if err != nil {
		return tinfo, err
	}

	data := struct {
		Method    string `json:"method"`
		Arguments struct {
			Location string `json:"location"`
			Ids      []int  `json:"ids"`
		} `json:"arguments"`
	}{}
	data.Method = "torrent-set-location"
	data.Arguments.Location = path
	data.Arguments.Ids = []int{tinfo.ID}

	b := new(bytes.Buffer)
	json.NewEncoder(b).Encode(data)
	req, err := t.makeRequest(b)
	if err != nil {
		return tinfo, err
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return tinfo, err
	}
	defer resp.Body.Close()
	return tinfo, err
}
