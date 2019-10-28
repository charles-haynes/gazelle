// Copyright © 2018 Charles Haynes <ceh@ceh.bz>
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package gazelle_test

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/charles-haynes/gazelle"
	"github.com/charles-haynes/whatapi"
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

var db *sqlx.DB

func TestMain(m *testing.M) {
	var err error
	db, err = sqlx.Connect("sqlite3", ":memory:")
	if err == nil {
		err = LoadTestDB(db)
	}
	if err != nil {
		fmt.Printf("Can't open in memory db: %s", err)
		os.Exit(-1)
	}
	defer db.Close()
	os.Exit(m.Run())
}

const (
	createArtists = `
CREATE TABLE artists (
    tracker              TEXT    NOT NULL,
    id                   INTEGER NOT NULL,
    name                 TEXT    NOT NULL,
    notificationsenabled BOOL, -- not in MusicInfo
    hasbookmarked        BOOL, -- not in MusicInfo
    image                TEXT, -- not in MusicInfo
    body                 TEXT, -- not in MusicInfo
    vanityhouse          BOOL, -- not in MusicInfo
    -- similarartists
    numgroups            INTEGER, -- not in MusicInfo
    numtorrents          INTEGER, -- not in MusicInfo
    numseeders           INTEGER, -- not in MusicInfo
    numleechers          INTEGER, -- not in MusicInfo
    numsnatches          INTEGER, -- not in MusicInfo
    PRIMARY KEY (tracker, id)) WITHOUT ROWID;
CREATE INDEX artists_name ON artists(name COLLATE NOCASE);
`
	createGroups = `
CREATE TABLE groups (
    tracker         TEXT     NOT NULL,
    wikibody        TEXT, --          not in artist, search, top10
    wikiimage       TEXT, --          not in search
    id              INTEGER  NOT NULL,
    name            TEXT     NOT NULL,
    year            INTEGER  NOT NULL,
    recordlabel     TEXT, --          not in search or top10
    cataloguenumber TEXT, --          not in search or top10
    releasetype     INTEGER  NOT NULL,
    categoryid      INTEGER, --       not search
    categoryname    TEXT, --          not in artist
    time            DATETIME, --      not in artist
    vanityhouse     BOOL     NOT NULL,
    isbookmarked    BOOL, --          not in torrent
    -- map artists to group
    tags            STRING   NOT NULL, -- concatenated with ,
    PRIMARY KEY(tracker, id)) WITHOUT ROWID;
CREATE INDEX groups_name ON groups(name COLLATE NOCASE);
`
	createArtistsGroups = `
CREATE TABLE artists_groups (
    tracker  TEXT NOT NULL,
    artistid INTEGER NOT NULL,
    groupid  INTEGER NOT NULL,
    role     TEXT NOT NULL,
    PRIMARY KEY (tracker, artistid, groupid),
    FOREIGN KEY(tracker, groupid) REFERENCES groups(tracker, id),
    FOREIGN KEY(tracker, artistid) REFERENCES artists(tracker, id)
) WITHOUT ROWID;
CREATE INDEX artists_groups_artistid ON artists_groups(tracker, artistid);
CREATE INDEX artists_groups_groupid ON artists_groups(tracker, groupid);
`
	createFiles = `
CREATE TABLE files (
    tracker   STRING  NOT NULL,
    torrentid INT     NOT NULL,
    name      STRING  NOT NULL,
    size      INTEGER NOT NULL,
    FOREIGN KEY(tracker, torrentid) REFERENCES torrents(tracker, id)
);
CREATE UNIQUE INDEX files_torrentid ON files(tracker, torrentid, name);
`
	createTorrents = `
CREATE TABLE torrents (
    tracker         TEXT     NOT NULL,
    id              INTEGER  NOT NULL,
    groupid         INTEGER  NOT NULL,
    hash            TEXT     UNIQUE,
    media           TEXT     NOT NULL,
    format          TEXT     NOT NULL,
    encoding        TEXT     NOT NULL,
    remastered      BOOL     NOT NULL,
    remasteryear    INTEGER  NOT NULL,
    remastertitle   TEXT     NOT NULL,
    remasterlabel   TEXT     NOT NULL,
    cataloguenumber TEXT,
    scene           BOOL     NOT NULL,
    haslog          BOOL     NOT NULL,
    hascue          BOOL     NOT NULL,
    logscore        INTEGER  NOT NULL,
    filecount       INTEGER  NOT NULL,
    size            INTEGER  NOT NULL,
    seeders         INTEGER  NOT NULL,
    leechers        INTEGER  NOT NULL,
    snatched        INTEGER  NOT NULL,
    freetorrent     BOOL     NOT NULL,
    reported        BOOL,
    time            DATETIME NOT NULL,
    description     TEXT,
    -- filelist
    filepath        TEXT,
    userid          INTEGER,
    username        TEXT,
    PRIMARY KEY(tracker, id),
    FOREIGN KEY(tracker, groupid) REFERENCES groups(tracker, id)) WITHOUT ROWID;
CREATE UNIQUE INDEX torrents_hash ON torrents(hash);
CREATE INDEX torrents_groupid ON torrents(tracker, groupid);
`
)

func LoadTestDB(db *sqlx.DB) error {
	_, err := db.Exec(`
PRAGMA foreign_keys=OFF;
BEGIN TRANSACTION;
` +
		createArtists +
		createArtistsGroups +
		createFiles +
		createGroups +
		createTorrents +
		`
COMMIT;
PRAGMA foreign_keys=ON;
`)
	return err

}

type MockWhatAPI struct {
	JSON  string
	Calls *[]string
}

func (m *MockWhatAPI) Called() {
	rpc := make([]uintptr, 1)
	runtime.Callers(2, rpc)
	frame, _ := runtime.CallersFrames(rpc).Next()
	n := strings.TrimPrefix(
		frame.Function,
		"github.com/charles-haynes/gazelle_test.MockWhatAPI.")
	*m.Calls = append(*m.Calls, n)
}

func (m MockWhatAPI) Contains(name string) bool {
	for _, v := range *m.Calls {
		if v == name {
			return true
		}
	}
	return false
}

func (m MockWhatAPI) Print() {
	for _, v := range *m.Calls {
		fmt.Printf("%s\n", v)
	}
}

func (m *MockWhatAPI) Reset() {
	*m.Calls = (*m.Calls)[:0]
}

var (
	errRequestFailed       = errors.New("Request failed")
	errRequestFailedReason = func(err string) error {
		return fmt.Errorf("Request failed: %s", err)
	}
)

func checkResponseStatus(status, errorStr string) error {
	if status != "success" {
		if errorStr != "" {
			return errRequestFailedReason(errorStr)
		}
		return errRequestFailed
	}
	return nil
}

func (m MockWhatAPI) GetJSON(requestURL string, responseObj interface{}) error {
	m.Called()
	return json.Unmarshal([]byte(m.JSON), responseObj)
}
func (m MockWhatAPI) Do(action string, params url.Values, result interface{}) error {
	m.Called()
	return nil
}
func (m MockWhatAPI) CreateDownloadURL(id int) (s string, e error) {
	m.Called()
	return
}
func (m MockWhatAPI) Login(username, password string) error {
	m.Called()
	return nil
}
func (m MockWhatAPI) Logout() error {
	m.Called()
	return nil
}
func (m MockWhatAPI) GetAccount() (a whatapi.Account, e error) {
	m.Called()
	return
}
func (m MockWhatAPI) GetMailbox(params url.Values) (M whatapi.Mailbox, e error) {
	m.Called()
	return
}
func (m MockWhatAPI) GetConversation(id int) (C whatapi.Conversation, e error) {
	m.Called()
	return
}
func (m MockWhatAPI) GetNotifications(params url.Values) (N whatapi.Notifications, e error) {
	m.Called()
	return
}
func (m MockWhatAPI) GetAnnouncements() (A whatapi.Announcements, e error) {
	m.Called()
	return
}
func (m MockWhatAPI) GetSubscriptions(params url.Values) (S whatapi.Subscriptions, e error) {
	m.Called()
	return
}
func (m MockWhatAPI) GetCategories() (C whatapi.Categories, e error) {
	m.Called()
	return
}
func (m MockWhatAPI) GetForum(id int, params url.Values) (F whatapi.Forum, e error) {
	m.Called()
	return
}
func (m MockWhatAPI) GetThread(id int, params url.Values) (T whatapi.Thread, e error) {
	m.Called()
	return
}
func (m MockWhatAPI) GetArtistBookmarks() (A whatapi.ArtistBookmarks, e error) {
	m.Called()
	return
}
func (m MockWhatAPI) GetTorrentBookmarks() (T whatapi.TorrentBookmarks, e error) {
	m.Called()
	return
}
func (m MockWhatAPI) GetArtist(id int, params url.Values) (A whatapi.Artist, e error) {
	m.Called()
	return
}
func (m MockWhatAPI) GetRequest(id int, params url.Values) (R whatapi.Request, e error) {
	m.Called()
	return
}
func (m MockWhatAPI) GetTorrent(id int, params url.Values) (G whatapi.GetTorrentStruct, e error) {
	m.Called()
	torrent := whatapi.TorrentResponse{}
	e = m.GetJSON(m.JSON, &torrent)
	if e != nil {
		return torrent.Response, e
	}
	return torrent.Response, checkResponseStatus(torrent.Status, torrent.Error)
	return
}
func (m MockWhatAPI) GetTorrentGroup(id int, params url.Values) (T whatapi.TorrentGroup, e error) {
	m.Called()
	return
}
func (m MockWhatAPI) SearchTorrents(searchStr string, params url.Values) (T whatapi.TorrentSearch, e error) {
	m.Called()
	return
}
func (m MockWhatAPI) SearchRequests(searchStr string, params url.Values) (R whatapi.RequestsSearch, e error) {
	m.Called()
	return
}
func (m MockWhatAPI) SearchUsers(searchStr string, params url.Values) (U whatapi.UserSearch, e error) {
	m.Called()
	return
}
func (m MockWhatAPI) GetTopTenTorrents(params url.Values) (T whatapi.TopTenTorrents, e error) {
	m.Called()
	return
}
func (m MockWhatAPI) GetTopTenTags(params url.Values) (T whatapi.TopTenTags, e error) {
	m.Called()
	return
}
func (m MockWhatAPI) GetTopTenUsers(params url.Values) (T whatapi.TopTenUsers, e error) {
	m.Called()
	return
}
func (m MockWhatAPI) GetSimilarArtists(id, limit int) (S whatapi.SimilarArtists, e error) {
	m.Called()
	return
}
func (m MockWhatAPI) ParseHTML(s string) (st string, e error) {
	m.Called()
	return
}

const torrent1JSON = `{"status":"success","response":{"group":{"wikiBody":"blah blah","wikiImage":"https:\/\/ptpimg.me\/yh5fqd.jpg","id":1,"name":"The Dark Side of the Moon","year":1973,"recordLabel":"","catalogueNumber":"","releaseType":1,"categoryId":1,"categoryName":"Music","time":"2019-08-28 17:46:53","vanityHouse":false,"isBookmarked":false,"musicInfo":{"composers":[],"dj":[],"artists":[{"id":1,"name":"Pink Floyd"}],"with":[],"conductor":[],"remixedBy":[],"producer":[]},"tags":["rock","experimental","progressive.rock","psychedelic","psychedelic.rock","space.rock","classic.rock","hard.rock","1970s","art.rock","british","staff.recs"]},"torrent":{"id":1,"infoHash":"C380B62A3EC6658597C56F45D596E8081B3F7A5C","media":"CD","format":"FLAC","encoding":"Lossless","remastered":true,"remasterYear":1988,"remasterTitle":"Japan MFSL UltraDisc #1, 24 Karat Gold","remasterRecordLabel":"Mobile Fidelity Sound Lab","remasterCatalogueNumber":"UDCD 517","scene":false,"hasLog":true,"hasCue":true,"logScore":70,"fileCount":12,"size":219114079,"seeders":100,"leechers":0,"snatched":414,"freeTorrent":false,"reported":false,"time":"2016-11-24 01:34:03","description":"[important]Staff: Technically trumped because EAC 0.95 logs are terrible. There is historic and sentimental value in keeping the first torrent ever uploaded to the site as well as a perfect modern rip. Take no action.[\/important]","fileList":"01 - Speak to Me.flac{{{3732587}}}|||02 -  Breathe.flac{{{14244409}}}|||03 - On the Run.flac{{{16541873}}}|||04 - Time.flac{{{35907465}}}|||05 -  The Great Gig in the Sky.flac{{{20671913}}}|||06 - Money.flac{{{37956922}}}|||07 -Us and Them.flac{{{39706774}}}|||08 - Any Colour You Like.flac{{{18736396}}}|||09 - Brain Damage.flac{{{20457034}}}|||10 - Eclipse.flac{{{11153655}}}|||Pink Floyd - Dark Side of the Moon.CUE{{{1435}}}|||Pink Floyd - Dark Side of the Moon.log{{{3616}}}","filePath":"Pink Floyd - Dark Side of the Moon (OMR MFSL 24k Gold Ultradisc II) fixed tags","userId":9,"username":"danger"}}}`

func TestArtistUpdate(t *testing.T) {
	a := gazelle.Artist{2717, "artist"}
	tx, err := db.Beginx()
	if err != nil {
		t.Error(err)
	}
	defer tx.Rollback()
	tracker := gazelle.Tracker{Name: "tracker"}
	var ta gazelle.Artist
	err = tx.Get(&ta, `SELECT name, id FROM artists WHERE id=?`, a.ID)
	if err == nil {
		t.Errorf("precondition failed artist with id %d exists", a.ID)
	}
	if err != sql.ErrNoRows {
		t.Error(err)
	}
	err = a.Update(tx, tracker)
	err = tx.Get(&ta, `SELECT name, id FROM artists WHERE id=?`, a.ID)
	if err != nil {
		t.Error(err)
	}
	if ta.ID != a.ID {
		t.Errorf("expected artist with id %d, got %d", a.ID, ta.ID)
	}
}

func TestArtistsNames(t *testing.T) {
	a := gazelle.Artists{
		Artists: map[string][]gazelle.Artist{
			"Artist": {{1, "artist1"}, {2, "artist2"}},
		},
	}
	res := a.Names()
	expected := []string{"artist1", "artist2"}
	if !reflect.DeepEqual(res, expected) {
		t.Errorf(`expected %v got %#v`, expected, res)
	}
}

func TestArtistsGetArtists(t *testing.T) {
	to := gazelle.Torrent{
		Group: gazelle.Group{
			Artists: gazelle.Artists{
				Tracker: gazelle.Tracker{Name: "tracker"},
			},
			ID: 2,
		},
	}
	expectedArtists := map[string][]gazelle.Artist{
		"Role": {{1, "artist"}},
	}
	_, err := db.Exec(`
DELETE FROM artists_groups;
DELETE FROM groups;
DELETE FROM artists;
INSERT INTO artists (tracker,id,name) VALUES("tracker",1,"artist");
INSERT INTO groups VALUES("tracker",NULL,NULL,2,"group",0,"","","",NULL,NULL,NULL,false,NULL,"");
INSERT INTO artists_groups VALUES("tracker",1,2,"Role");
`)
	if err != nil {
		t.Error(err)
	}
	err = to.GetArtists(db)
	if err != nil {
		t.Error(err)
	}
	if !reflect.DeepEqual(expectedArtists, to.Artists.Artists) {
		t.Errorf("expected %v got %v", expectedArtists, to.Artists.Artists)
	}
}

func TestArtistsDisplayName(t *testing.T) {
	a := gazelle.Artists{Artists: map[string][]gazelle.Artist{}}
	if r := a.DisplayName(); r != "" {
		t.Errorf("expected display name \"\", got \"%s\"", r)
	}
	a.Artists["Artist"] = append(
		a.Artists["Artist"], gazelle.Artist{1, "Artist1"})
	if r := a.DisplayName(); r != "Artist1" {
		t.Errorf("expected display name foo, got %s", r)
	}
	a.Artists["Artist"] = append(
		a.Artists["Artist"], gazelle.Artist{2, "Artist2"})
	if r := a.DisplayName(); r != "Artist1 & Artist2" {
		t.Errorf("expected display name foo & bar, got %s", r)
	}
	a.Artists["Artist"] = append(
		a.Artists["Artist"], gazelle.Artist{3, "Artist3"})
	if r := a.DisplayName(); r != "Various Artists" {
		t.Errorf("expected display Various Artists, got %s", r)
	}
}

func TestArtistsUpdate(t *testing.T) {
	tx, err := db.Beginx()
	if err != nil {
		t.Error(err)
	}
	defer tx.Rollback()
	_, err = tx.Exec(`
DELETE FROM artists_groups;
DELETE FROM groups;
DELETE FROM artists;
`)
	if err != nil {
		t.Error(err)
	}
	a := gazelle.Artists{
		Tracker: gazelle.Tracker{Name: "tracker"},
		Artists: map[string][]gazelle.Artist{
			"Artist": {{1, "artist1"}, {2, "artist2"}},
		},
	}
	err = a.Update(tx)
	if err != nil {
		t.Error(err)
	}
	var ta []gazelle.Artist
	err = tx.Select(&ta, `SELECT name, id FROM artists`)
	if err != nil {
		t.Error(err)
	}
	expected := []gazelle.Artist{{1, "artist1"}, {2, "artist2"}}
	if !reflect.DeepEqual(expected, a.Artists["Artist"]) {
		t.Errorf("expected %v got %v", expected, a.Artists["Artist"])
	}
}

func TestNewMusicInfo(t *testing.T) {
	tracker := gazelle.Tracker{Name: "tracker"}
	mi := whatapi.MusicInfo{
		Artists: []whatapi.MusicInfoStruct{{1, "artist1"}, {2, "artist2"}},
		With:    []whatapi.MusicInfoStruct{{3, "artist3"}},
	}
	expected := gazelle.Artists{
		Tracker: tracker,
		Artists: map[string][]gazelle.Artist{
			"Composer":  {},
			"DJ":        {},
			"Artist":    {{1, "artist1"}, {2, "artist2"}},
			"With":      {{3, "artist3"}},
			"Conductor": {},
			"RemixedBy": {},
			"Producer":  {},
		},
	}
	a := gazelle.NewMusicInfo(tracker, mi)
	if !reflect.DeepEqual(expected, a) {
		t.Errorf("expected %v got %v", expected, a)
	}
}

func TestNewExtendedArtistMap(t *testing.T) {
	tracker := gazelle.Tracker{Name: "tracker"}
	am := whatapi.ExtendedArtistMap{
		"1": []whatapi.ArtistGroupArtist{
			{1, "artist1", 4},
			{2, "artist2", 5},
		},
		"2": []whatapi.ArtistGroupArtist{{3, "artist3", 6}},
	}
	expected := gazelle.Artists{
		Tracker: tracker,
		Artists: map[string][]gazelle.Artist{
			"Artist": {{1, "artist1"}, {2, "artist2"}},
			"With":   {{3, "artist3"}},
		},
	}
	a := gazelle.NewExtendedArtistMap(tracker, am)
	if !reflect.DeepEqual(expected, a) {
		t.Errorf("expected %v got %v", expected, a)
	}
}

func TestGroupReleaseType(t *testing.T) {
	g := gazelle.Group{
		Artists: gazelle.Artists{
			Tracker: gazelle.Tracker{
				ReleaseTypes: map[int64]string{1: "release"},
			},
		},
		ReleaseTypeF: 1,
	}
	expected := "release"
	r := g.ReleaseType()
	if expected != r {
		t.Errorf("expected %v got %v", expected, r)
	}
}

func TestTorrentShortName(t *testing.T) {
	to := gazelle.Torrent{
		Group: gazelle.Group{
			Artists: gazelle.Artists{
				Tracker: gazelle.Tracker{Name: "tracker"},
			},
		},
		ID: 1,
	}
	expected := "tracker-1"
	r := to.ShortName()
	if expected != r {
		t.Errorf("expected %v got %v", expected, r)
	}
}

func TestTorrentFill_BadTorrentID(t *testing.T) {
	tx, err := db.Beginx()
	if err != nil {
		t.Error(err)
	}
	defer tx.Rollback()
	m := MockWhatAPI{
		JSON:  `{"status":"failure","error":"bad id parameter"}`,
		Calls: &[]string{},
	}
	to := gazelle.Torrent{
		Group: gazelle.Group{
			Artists: gazelle.Artists{
				Tracker: gazelle.Tracker{
					WhatAPI: m,
					Name:    "tracker",
				},
			},
		},
		ID: 1,
	}
	err = to.Fill(tx)
	if err == nil {
		t.Errorf("expected error bad id parameter, but got nil")
	}
	if !strings.Contains(err.Error(), "bad id parameter") {
		t.Errorf("expected error bad id parameter, but got %v", err)
	}
	if !m.Contains("GetJSON") {
		t.Errorf("expected to fetch JSON")
	}
}

func TestTorrentFill_AlreadyFilled(t *testing.T) {
	tx, err := db.Beginx()
	if err != nil {
		t.Error(err)
	}
	defer tx.Rollback()
	m := MockWhatAPI{
		JSON:  `{"status":"failure","error":"bad id parameter"}`,
		Calls: &[]string{},
	}
	s := "file/path"
	to := gazelle.Torrent{
		Group: gazelle.Group{
			Artists: gazelle.Artists{
				Tracker: gazelle.Tracker{
					WhatAPI: m,
					Name:    "tracker",
				},
			},
		},
		ID:       1,
		Files:    []whatapi.FileStruct{},
		FilePath: &s,
	}

	err = to.Fill(tx)
	if err != nil {
		t.Error(err)
	}
	if m.Contains("GetTorrent") {
		t.Errorf("expected not to call GetTorrent")
	}
	if m.Contains("GetJSON") {
		t.Errorf("expected not to call GetJSON")
	}
}

func TestTorrentFill_NeedsFilling(t *testing.T) {
	tx, err := db.Beginx()
	if err != nil {
		t.Error(err)
	}
	defer tx.Rollback()
	m := MockWhatAPI{
		JSON:  torrent1JSON,
		Calls: &[]string{},
	}
	to := gazelle.Torrent{
		Group: gazelle.Group{
			Artists: gazelle.Artists{
				Tracker: gazelle.Tracker{
					WhatAPI: m,
					Name:    "tracker",
				},
			},
		},
		ID: 1,
	}
	m.Reset()
	err = to.Fill(tx)
	if err != nil {
		t.Error(err)
	}
	if !m.Contains("GetTorrent") {
		t.Errorf("expected to Get Torrent")
	}
	if !m.Contains("GetJSON") {
		t.Errorf("expected to fetch JSON")
	}
	if to.Files == nil {
		t.Errorf("expected to fill Files")
	}
	if to.FilePath == nil {
		t.Errorf("expected to fill FilePath")
	}
}

func TestGroupUpdateArtistsGroups_NoArtists(t *testing.T) {
	tx, err := db.Beginx()
	if err != nil {
		t.Error(err)
	}
	defer tx.Rollback()
	_, err = tx.Exec(`
DELETE FROM artists_groups;
DELETE FROM groups;
DELETE FROM artists;
`)
	if err != nil {
		t.Error(err)
	}
	g := gazelle.Group{
		Artists: gazelle.Artists{
			Artists: map[string][]gazelle.Artist{},
		},
	}
	err = g.UpdateArtistsGroups(tx)
	if err != nil {
		t.Error(err)
	}
	var count int
	err = tx.Get(&count, `SELECT count(*) FROM artists_groups`)
	if count != 0 {
		t.Errorf("expected artists_groups to have 0 rows, got %d", count)
	}
}

func TestGroupUpdateArtistsGroups(t *testing.T) {
	tx, err := db.Beginx()
	if err != nil {
		t.Error(err)
	}
	defer tx.Rollback()
	_, err = tx.Exec(`
DELETE FROM artists_groups;
DELETE FROM groups;
DELETE FROM artists;
INSERT INTO artists (tracker,id,name) VALUES("tracker",1,"artist1");
INSERT INTO artists (tracker,id,name) VALUES("tracker",2,"artist2");
INSERT INTO groups VALUES("tracker",NULL,NULL,3,"baz",0,"","","",NULL,NULL,NULL,false,NULL,"");
`)
	if err != nil {
		t.Error(err)
	}
	g := gazelle.Group{
		Artists: gazelle.Artists{
			Tracker: gazelle.Tracker{
				Name: "tracker",
			},
			Artists: map[string][]gazelle.Artist{
				"role": {{1, "artist1"}, {2, "artist2"}},
			},
		},
		ID: 3,
	}
	err = g.UpdateArtistsGroups(tx)
	if err != nil {
		t.Error(err)
	}
	type ag struct {
		Tracker  string
		ArtistID int64
		GroupID  int64
		Role     string
	}
	var r []ag
	err = tx.Select(&r, `SELECT * from artists_groups`)
	if err != nil {
		t.Error(err)
	}
	expected := []ag{
		{"tracker", 1, 3, "role"},
		{"tracker", 2, 3, "role"},
	}
	if !reflect.DeepEqual(expected, r) {
		t.Errorf("expected %v got %v", expected, r)
	}
}

func TestBint(t *testing.T) {
	if r := gazelle.Bint(true); r != 1 {
		t.Errorf("expected %v got %v", 1, r)
	}
	if r := gazelle.Bint(false); r != 0 {
		t.Errorf("expected %v got %v", 0, r)
	}
}

func TestGroupUpdate(t *testing.T) {
	tx, err := db.Beginx()
	if err != nil {
		t.Error(err)
	}
	defer tx.Rollback()
	_, err = tx.Exec(`
DELETE FROM artists_groups;
DELETE FROM groups;
DELETE FROM artists;
`)
	if err != nil {
		t.Error(err)
	}
	var count int
	err = tx.Get(&count, `SELECT COUNT(*) FROM groups`)
	if err != nil {
		t.Error(err)
	}
	if count != 0 {
		t.Errorf("expected group to have 0 rows, got %d", count)
	}
	g := gazelle.Group{
		Artists: gazelle.Artists{
			Tracker: gazelle.Tracker{
				Name: "tracker",
			},
			Artists: map[string][]gazelle.Artist{
				"role": {{1, "artist1"}, {2, "artist2"}},
			},
		},
		ID:   3,
		Name: "group",
	}
	err = g.Update(tx)
	if err != nil {
		t.Error(err)
	}
	type DBGroup struct {
		Tracker         string
		Wikibody        *string
		Wikiimage       *string
		ID              int64
		Name            string
		Year            int64
		Recordlabel     *string
		Cataloguenumber *string
		Releasetype     int64
		Categoryid      *int64
		Categoryname    *string
		Time            *string
		Vanityhouse     bool
		Isbookmarked    *bool
		Tags            string
	}
	var r []DBGroup
	err = tx.Select(&r, `SELECT * FROM groups`)
	if err != nil {
		t.Error(err)
	}
	expected := []DBGroup{
		{
			Tracker:         "tracker",
			Wikibody:        nil,
			Wikiimage:       nil,
			ID:              3,
			Name:            "group",
			Year:            0,
			Recordlabel:     nil,
			Cataloguenumber: nil,
			Releasetype:     0,
			Categoryid:      nil,
			Categoryname:    nil,
			Time:            nil,
			Vanityhouse:     false,
			Isbookmarked:    nil,
			Tags:            "",
		},
	}
	if !reflect.DeepEqual(expected, r) {
		t.Errorf("expected %v but got %v", expected, r)
	}
}

func GroupsEqual(g1, g2 gazelle.Group) error {
	if reflect.DeepEqual(g1, g2) {
		return nil
	}
	if !reflect.DeepEqual(g1.Artists, g2.Artists) {
		return fmt.Errorf("Artists")
	}
	if !reflect.DeepEqual(g1.ID, g2.ID) {
		return fmt.Errorf("ID")
	}
	if !reflect.DeepEqual(g1.Name, g2.Name) {
		return fmt.Errorf("Name")
	}
	if !reflect.DeepEqual(g1.Year, g2.Year) {
		return fmt.Errorf("Year")
	}
	if !reflect.DeepEqual(g1.RecordLabel, g2.RecordLabel) {
		return fmt.Errorf("RecordLabel")
	}
	if !reflect.DeepEqual(g1.CatalogueNumber, g2.CatalogueNumber) {
		return fmt.Errorf("CatalogueNumber")
	}
	if !reflect.DeepEqual(g1.ReleaseTypeF, g2.ReleaseTypeF) {
		return fmt.Errorf("ReleaseTypeF")
	}
	if !reflect.DeepEqual(g1.CategoryID, g2.CategoryID) {
		return fmt.Errorf("CategoryID")
	}
	if !reflect.DeepEqual(g1.CategoryName, g2.CategoryName) {
		return fmt.Errorf("CategoryName")
	}
	if !reflect.DeepEqual(g1.Time, g2.Time) {
		return fmt.Errorf("Time")
	}
	if !reflect.DeepEqual(g1.VanityHouse, g2.VanityHouse) {
		return fmt.Errorf("VanityHouse")
	}
	if !reflect.DeepEqual(g1.WikiImage, g2.WikiImage) {
		return fmt.Errorf("WikiImage")
	}
	if !reflect.DeepEqual(g1.WikiBody, g2.WikiBody) {
		return fmt.Errorf("WikiBody")
	}
	if !reflect.DeepEqual(g1.IsBookmarked, g2.IsBookmarked) {
		return fmt.Errorf("IsBookmarked")
	}
	if !reflect.DeepEqual(g1.Tags, g2.Tags) {
		return fmt.Errorf("Tags")
	}
	return fmt.Errorf("unknown, did you leave out a field in GroupEqual?")
}

func TestNewGroupStruct(t *testing.T) {
	tracker := gazelle.Tracker{Name: "tracker"}
	gs := whatapi.GroupStruct{
		WikiImageF:       "wikiimage",
		WikiBodyF:        "wikibody",
		IDF:              1,
		NameF:            "name",
		YearF:            2,
		RecordLabelF:     "recordlabel",
		CatalogueNumberF: "cataloguenumber",
		ReleaseTypeF:     7,
		CategoryID:       3,
		CategoryName:     "categoryname",
		Time:             "1234-05-06 07:08:09",
		VanityHouse:      true,
		MusicInfo: whatapi.MusicInfo{
			Artists: []whatapi.MusicInfoStruct{
				{4, "artist4"},
				{5, "artist5"},
			},
			With: []whatapi.MusicInfoStruct{
				{6, "artist6"},
			},
		},
		IsBookmarked: true,
		TagsF:        []string{"tag1", "tag2"},
	}
	r, err := gazelle.NewGroupStruct(tracker, gs)
	if err != nil {
		t.Error(err)
	}
	recordLabel := "recordlabel"
	catalogNumber := "cataloguenumber"
	categoryID := int64(3)
	categoryName := "categoryname"
	wikiImage := "wikiimage"
	time := time.Date(1234, time.May, 6, 7, 8, 9, 0, time.UTC)
	wikiBody := "wikibody"
	isBookmarked := true
	expected := gazelle.Group{
		Artists: gazelle.Artists{
			Tracker: gazelle.Tracker{
				Name: "tracker",
			},
			Artists: map[string][]gazelle.Artist{
				"Artist":    {{4, "artist4"}, {5, "artist5"}},
				"Composer":  {},
				"Conductor": {},
				"DJ":        {},
				"Producer":  {},
				"RemixedBy": {},
				"With":      {{6, "artist6"}},
			},
		},
		ID:              1,
		Name:            "name",
		Year:            2,
		RecordLabel:     &recordLabel,
		CatalogueNumber: &catalogNumber,
		ReleaseTypeF:    int64(7),
		CategoryID:      &categoryID,
		CategoryName:    &categoryName,
		Time:            &time,
		VanityHouse:     true,
		WikiImage:       &wikiImage,
		WikiBody:        &wikiBody,
		IsBookmarked:    &isBookmarked,
		Tags:            "tag1,tag2",
	}
	if err := GroupsEqual(expected, r); err != nil {
		t.Errorf("expected %v got %v, differs in %s", expected, r, err)
	}
}

func TestUpdateFiles(t *testing.T) {
	tx, err := db.Beginx()
	if err != nil {
		t.Error(err)
	}
	defer tx.Rollback()
	_, err = tx.Exec(`
DELETE FROM artists_groups;
DELETE FROM artists;
DELETE FROM groups;
DELETE FROM torrents;
DELETE FROM files;
INSERT INTO groups VALUES("tracker",NULL,NULL,2,"group",0,"","","",NULL,NULL,NULL,false,NULL,"");
INSERT INTO torrents VALUES ("tracker",1,2,"","","","",false,0,"","",NULL,false,false,false,0,0,0,0,0,0,false,NULL,"1234-05-06 07:08:09",NULL,NULL,NULL,NULL);
`)
	if err != nil {
		t.Error(err)
	}
	to := gazelle.Torrent{
		Group: gazelle.Group{
			Artists: gazelle.Artists{
				Tracker: gazelle.Tracker{Name: "tracker"},
			},
			ID: 2,
		},
		ID:    1,
		Files: []whatapi.FileStruct{{"file3", 3}, {"file4", 4}},
	}
	err = to.UpdateFiles(tx)
	if err != nil {
		t.Error(err)
	}
	type file struct {
		Tracker   string
		TorrentID int64
		Name      string
		Size      int64
	}
	var r []file
	err = tx.Select(&r, `SELECT * FROM files`)
	if err != nil {
		t.Error(err)
	}
	expected := []file{
		{"tracker", 1, "file3", 3},
		{"tracker", 1, "file4", 4},
	}
	if !reflect.DeepEqual(expected, r) {
		t.Errorf("expected %v got %v", expected, r)
	}
}

func TestTorrentUpdate(t *testing.T) {
	tx, err := db.Beginx()
	if err != nil {
		t.Error(err)
	}
	defer tx.Rollback()
	_, err = tx.Exec(`
DELETE FROM artists_groups;
DELETE FROM artists;
DELETE FROM groups;
DELETE FROM torrents;
DELETE FROM files;
`)
	if err != nil {
		t.Error(err)
	}
	hash := "hash"
	time := time.Date(1234, time.May, 6, 7, 8, 9, 0, time.UTC)
	to := gazelle.Torrent{
		Group: gazelle.Group{
			Artists: gazelle.Artists{
				Tracker: gazelle.Tracker{Name: "tracker"},
			},
			ID: 2,
		},
		ID:                      1,
		Hash:                    &hash,
		Media:                   "media",
		Format:                  "format",
		Encoding:                "encoding",
		Remastered:              true,
		RemasterYear:            4321,
		RemasterTitle:           "remastertitle",
		RemasterRecordLabel:     "remasterlabel",
		RemasterCatalogueNumber: nil,
		Scene:                   true,
		HasLog:                  true,
		HasCue:                  false,
		LogScore:                100,
		LogChecksum:             nil,
		FileCount:               3,
		Size:                    4,
		Seeders:                 5,
		Leechers:                6,
		Snatched:                0,
		FreeTorrent:             false,
		Reported:                nil,
		Time:                    time,
		Description:             nil,
		FilePath:                nil,
		UserID:                  nil,
		Username:                nil,
		Files:                   []whatapi.FileStruct{{"file3", 3}, {"file4", 4}},
	}
	err = to.Update(tx)
	if err != nil {
		t.Error(err)
	}
	type DBTorrent struct {
		Tracker         string
		Id              int64
		Groupid         int64
		Hash            string
		Media           string
		Format          string
		Encoding        string
		Remastered      bool
		Remasteryear    int64
		Remastertitle   string
		Remasterlabel   string
		Cataloguenumber *string
		Scene           bool
		Haslog          bool
		Hascue          bool
		Logscore        int64
		Filecount       int64
		Size            int64
		Seeders         int64
		Leechers        int64
		Snatched        int64
		Freetorrent     bool
		Reported        *bool
		Time            string
		Description     *string
		Filepath        *string
		Userid          *int64
		Username        *string
	}
	var r []DBTorrent
	err = tx.Select(&r, `SELECT * FROM torrents`)
	expected := []DBTorrent{
		{
			Tracker:         "tracker",
			Id:              1,
			Groupid:         2,
			Hash:            "hash",
			Media:           "media",
			Format:          "format",
			Encoding:        "encoding",
			Remastered:      true,
			Remasteryear:    4321,
			Remastertitle:   "remastertitle",
			Remasterlabel:   "remasterlabel",
			Cataloguenumber: nil,
			Scene:           true,
			Haslog:          true,
			Hascue:          false,
			Logscore:        100,
			Filecount:       3,
			Size:            4,
			Seeders:         5,
			Leechers:        6,
			Snatched:        0,
			Freetorrent:     false,
			Reported:        nil,
			Time:            "1234-05-06T07:08:09Z",
			Description:     nil,
			Filepath:        nil,
			Userid:          nil,
			Username:        nil,
		},
	}
	if !reflect.DeepEqual(expected, r) {
		t.Errorf("expected %v got %v", expected, r)
	}
}
