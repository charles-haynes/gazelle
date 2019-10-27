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

func TestArtistUpdate(t *testing.T) {
	a := gazelle.Artist{2717, "foo"}
	tx, err := db.Beginx()
	if err != nil {
		t.Error(err)
	}
	defer tx.Rollback()
	tracker := gazelle.Tracker{Name: "bar"}
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
			"Artist": {{2717, "foo"}, {3414, "bar"}},
		},
	}
	res := a.Names()
	if !reflect.DeepEqual(res, []string{"foo", "bar"}) {
		t.Errorf(`expected {"foo", "bar"} got %#v`, res)
	}
}

func TestArtistsGetArtists(t *testing.T) {
	to := gazelle.Torrent{
		Group: gazelle.Group{
			Artists: gazelle.Artists{
				Tracker: gazelle.Tracker{Name: "bar"},
			},
			ID: 2717,
		},
	}
	expectedArtists := map[string][]gazelle.Artist{
		"Artist": {{3414, "foo"}},
	}
	_, err := db.Exec(`
DELETE FROM artists_groups;
DELETE FROM groups;
DELETE FROM artists;
INSERT INTO artists (tracker,id,name) VALUES("bar",3414,"foo");
INSERT INTO groups VALUES("bar",NULL,NULL,2717,"baz",0,"","","",NULL,NULL,NULL,false,NULL,"");
INSERT INTO artists_groups VALUES("bar",3414,2717,"Artist");
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
		a.Artists["Artist"], gazelle.Artist{2717, "foo"})
	if r := a.DisplayName(); r != "foo" {
		t.Errorf("expected display name foo, got %s", r)
	}
	a.Artists["Artist"] = append(
		a.Artists["Artist"], gazelle.Artist{3414, "bar"})
	if r := a.DisplayName(); r != "foo & bar" {
		t.Errorf("expected display name foo & bar, got %s", r)
	}
	a.Artists["Artist"] = append(
		a.Artists["Artist"], gazelle.Artist{5772, "baz"})
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
		Tracker: gazelle.Tracker{Name: "bar"},
		Artists: map[string][]gazelle.Artist{
			"Artist": {{2717, "foo"}, {3414, "bar"}},
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
	expected := []gazelle.Artist{{2717, "foo"}, {3414, "bar"}}
	if !reflect.DeepEqual(expected, a.Artists["Artist"]) {
		t.Errorf("expected %v got %v", expected, a.Artists["Artist"])
	}
}

func TestNewMusicInfo(t *testing.T) {
	tracker := gazelle.Tracker{Name: "foo"}
	mi := whatapi.MusicInfo{
		Artists: []whatapi.MusicInfoStruct{{1, "bar"}, {2, "baz"}},
		With:    []whatapi.MusicInfoStruct{{3, "bletch"}},
	}
	expected := gazelle.Artists{
		Tracker: tracker,
		Artists: map[string][]gazelle.Artist{
			"Composer":  {},
			"DJ":        {},
			"Artist":    {{1, "bar"}, {2, "baz"}},
			"With":      {{3, "bletch"}},
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
	tracker := gazelle.Tracker{Name: "foo"}
	am := whatapi.ExtendedArtistMap{
		"1": []whatapi.ArtistGroupArtist{{1, "bar", 4}, {2, "baz", 5}},
		"2": []whatapi.ArtistGroupArtist{{3, "bletch", 6}},
	}
	expected := gazelle.Artists{
		Tracker: tracker,
		Artists: map[string][]gazelle.Artist{
			"Artist": {{1, "bar"}, {2, "baz"}},
			"With":   {{3, "bletch"}},
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
				ReleaseTypes: map[int64]string{2717: "foo"},
			},
		},
		ReleaseTypeF: 2717,
	}
	expected := "foo"
	r := g.ReleaseType()
	if expected != r {
		t.Errorf("expected %v got %v", expected, r)
	}
}

func TestTorrentShortName(t *testing.T) {
	to := gazelle.Torrent{
		Group: gazelle.Group{
			Artists: gazelle.Artists{
				Tracker: gazelle.Tracker{Name: "foo"},
			},
		},
		ID: 2717,
	}
	expected := "foo-2717"
	r := to.ShortName()
	if expected != r {
		t.Errorf("expected %v got %v", expected, r)
	}
}

func TestTorrentFill(t *testing.T) {
	tx, err := db.Beginx()
	if err != nil {
		t.Error(err)
	}
	defer tx.Rollback()
	// three cases
	// 1 bad torrent id
	// 2 already filled
	// 3 needs filling
	// need to inject JSON getter and test that we fill from json
	// (though that's duplicating GetTorrent)
	// then check that the DB gets upated
	// (though that duplicates Update)

	// case 1 bad torrent id
	m := MockWhatAPI{
		JSON:  `{"status":"failure","error":"bad id parameter"}`,
		Calls: &[]string{},
	}
	to := gazelle.Torrent{
		Group: gazelle.Group{
			Artists: gazelle.Artists{
				Tracker: gazelle.Tracker{
					WhatAPI: m,
					Name:    "foo",
				},
			},
		},
		ID: 3414,
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

	// 2 already filled
	m = MockWhatAPI{
		JSON:  `{"status":"failure","error":"bad id parameter"}`,
		Calls: &[]string{},
	}
	s := "bar"
	to = gazelle.Torrent{
		Group: gazelle.Group{
			Artists: gazelle.Artists{
				Tracker: gazelle.Tracker{
					WhatAPI: m,
					Name:    "foo",
				},
			},
		},
		ID:       3414,
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

	// 3 needs filling
	m = MockWhatAPI{
		JSON:  torrent1JSON,
		Calls: &[]string{},
	}
	to = gazelle.Torrent{
		Group: gazelle.Group{
			Artists: gazelle.Artists{
				Tracker: gazelle.Tracker{
					WhatAPI: m,
					Name:    "foo",
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

func LoadTestDB(db *sqlx.DB) error {
	_, err := db.Exec(`
PRAGMA foreign_keys=OFF;
BEGIN TRANSACTION;
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

CREATE TABLE groups (
    tracker         TEXT     NOT NULL,
    wikibody        TEXT, -- not in artist
    wikiimage       TEXT, -- not in artist
    id              INTEGER  NOT NULL,
    name            TEXT     NOT NULL,
    year            INTEGER  NOT NULL,
    recordlabel     TEXT     NOT NULL,
    cataloguenumber TEXT     NOT NULL,
    releasetype     INTEGER  NOT NULL,
    categoryid      INTEGER, -- not in artist
    categoryname    TEXT, --  not in artist
    time            DATETIME, -- not in artist
    vanityhouse     BOOL     NOT NULL,
    isbookmarked    BOOL, -- not in torrent
    -- map artists to group
    tags            STRING   NOT NULL, -- concatenated with ,
    PRIMARY KEY(tracker, id)) WITHOUT ROWID;
CREATE INDEX groups_name ON groups(name COLLATE NOCASE);

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
	errRequestFailedReason = func(err string) error { return fmt.Errorf("Request failed: %s", err) }
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

const torrent1JSON = `{"status":"success","response":{"group":{"wikiBody":"<span class=\"size5\"><strong><a href=\"artist.php?artistname=Pink+Floyd\">Pink Floyd<\/a> &ndash; The Dark Side Of The Moon<\/strong><\/span><br \/>\r\n<br \/>\r\n<strong>Label\/Cat#:<\/strong> <a href=\"\/torrents.php?recordlabel=Harvest\">Harvest<\/a> &ndash; SHVL 804, <a href=\"\/torrents.php?recordlabel=Harvest\">Harvest<\/a> &ndash; 1E 064 o 05249<br \/>\r\n<strong>Country:<\/strong> UK<br \/>\r\n<strong>Year:<\/strong> 24 Mar 1973<br \/>\r\n<strong>Genre:<\/strong> <a href=\"\/torrents.php?taglist=Rock\">Rock<\/a><br \/>\r\n<strong>Style:<\/strong> <a href=\"\/torrents.php?taglist=Prog.Rock\">Prog Rock<\/a><br \/>\r\n<strong>Format:<\/strong> Vinyl, LP, Album, Gatefold Sleeve<br \/>\r\n<br \/>\r\n<span class=\"size4\"><strong>Tracklist<\/strong><\/span><br \/>\r\n<br \/>\r\n<strong>A1<\/strong> &ndash; Speak To Me<br \/>\r\n<strong>A2<\/strong> &ndash; Breathe<br \/>\r\n<strong>A3<\/strong> &ndash; On The Run<br \/>\r\n<strong>A4<\/strong> &ndash; Time<br \/>\r\n<strong>A5<\/strong> &ndash; The Great Gig In The Sky<br \/>\r\n<strong>B1<\/strong> &ndash; Money<br \/>\r\n<strong>B2<\/strong> &ndash; Us And Them<br \/>\r\n<strong>B3<\/strong> &ndash; Any Colour You Like<br \/>\r\n<strong>B4<\/strong> &ndash; Brain Damage<br \/>\r\n<strong>B5<\/strong> &ndash; Eclipse<br \/>\r\n<br \/>\r\nMore information: <a rel=\"noreferrer\" target=\"_blank\" href=\"http:\/\/www.discogs.com\/Pink-Floyd-The-Dark-Side-Of-The-Moon\/release\/1873013\">http:\/\/www.discogs.com\/Pink-Floyd-The-Dark-Side-Of-The-Moon\/release\/1873013<\/a><br \/>\r\n<br \/>\r\nMaster release: <a rel=\"noreferrer\" target=\"_blank\" href=\"https:\/\/www.discogs.com\/Pink-Floyd-The-Dark-Side-Of-The-Moon\/master\/10362\">https:\/\/www.discogs.com\/Pink-Floyd-The-Dark-Side-Of-The-Moon\/master\/10362<\/a><br \/>\r\n<br \/>\r\n<strong>About this album<\/strong>: <a href=\"javascript:void(0);\" onclick=\"BBCode.spoiler(this);\">Show<\/a><blockquote class=\"hidden spoiler\">The Dark Side of the Moon is the eighth album by English rock band Pink Floyd, released on 1 March 1973 by Harvest Records. The album built on ideas explored in earlier recordings and live shows, but lacks the extended instrumental excursions following the departure of founding member and principal contributor, Syd Barrett, in 1968 that characterised their earlier work. It thematically explores conflict, greed, the passage of time, and mental illness, the latter partly inspired by Barrett&#39;s deteriorating mental state.<br \/>\r\n<br \/>\r\nDeveloped during live performances, an early version was premiered several months before recording began; new material was recorded in two sessions in 1972 and 1973 at Abbey Road in London. The group used some advanced recording techniques at the time, including multitrack recording and tape loops. Analogue synthesizers were prominent in several tracks, and snippets from recorded interviews with Pink Floyd&#39;s road crew and others provided philosophical quotations throughout. Engineer Alan Parsons was responsible for many distinctively notable sonic aspects and the recruitment of singer Clare Torry. The album&#39;s iconic sleeve, designed by Storm Thorgerson, depicts a prism spectrum and represents the band&#39;s lighting, the record&#39;s thematic material, and keyboardist Richard Wright&#39;s &quot;simple and bold&quot; design request.<br \/>\r\n<br \/>\r\nThe album was an immediate commercial and critical success; it topped the Billboard Top LPs &amp; Tapes chart for a week and remained in the chart for 741 weeks from 1973 to 1988. With an estimated 45 million copies sold, it is Pink Floyd&#39;s most commercially successful album and one of the best-selling worldwide. It has been remastered and re-released twice, and covered in its entirety by several other acts. It produced two singles, &quot;Money&quot; and &quot;Us and Them&quot;, and is the band&#39;s most popular album among fans and critics, being ranked as one of the greatest albums of all time.<br \/>\r\n<br \/>\r\nThe Dark Side of the Moon explores the nature of the human experience. For example, &ldquo;Speak to Me \/ Breathe&rdquo; is about birth and being a child with new earthly experiences. &ldquo;Time&rdquo; deals with growing older and the overwhelmingly fast approach of death &ndash; youth being gone before one even realizes it. &ldquo;The Great Gig in the Sky&rdquo; deals with thoughts of death and religion, &ldquo;Money&rdquo; deals with consumerism with tongue-in-cheek lyrics and wealth-related sound effects. &ldquo;Us and Them&rdquo; deals with conflict, ethnocentrism, and the belief that a person&rsquo;s self is &ldquo;always in the right&rdquo;. &ldquo;Brain Damage&rdquo; and &ldquo;Eclipse&rdquo; deal with growing too old to be who one once was, and finally, death.<br \/>\r\n<br \/>\r\nRoger Waters wrote all of the lyrics in the album and created the early demo tracks in a small garden shed-turned-recording studio at his home. It was in there he also created the intro to &ldquo;Money&rdquo; by experimenting with dropping a range of monetary objects. All four members of Pink Floyd, which included guitarist David Gilmour, drummer Nick Mason, bassist and lyricist Roger Waters, and keyboardist Richard Wright, had some form of participation in the writing and production of the album, which is a rarity among later Pink Floyd albums. However, it is the first of five consecutive Pink Floyd albums with lyrics completely credited to Roger Waters.<br \/>\r\n<br \/>\r\nAlthough The Dark Side of the Moon was the planned title of the album, upon the discovery that the band Medicine Head was to release an album of the same name in 1972, the year prior to The Dark Side of the Moon&rsquo;s release, the band changed the album&rsquo;s title to Eclipse: A Piece for Assorted Lunatics. However, the Medicine Head album flopped, so Pink Floyd reverted to the original title.<br \/>\r\n<br \/>\r\nThe only time there is a gap of silence on the whole album is between &ldquo;The Great Gig in the Sky&rdquo; and &ldquo;Money&rdquo;, where there is a side change on the LP.<\/blockquote><br \/>\r\n<br \/>\r\n<strong>Review<\/strong>: <a href=\"javascript:void(0);\" onclick=\"BBCode.spoiler(this);\">Show<\/a><blockquote class=\"hidden spoiler\">By condensing the sonic explorations of Meddle to actual songs and adding a lush, immaculate production to their trippiest instrumental sections, Pink Floyd inadvertently designed their commercial breakthrough with Dark Side of the Moon. The primary revelation of Dark Side of the Moon is what a little focus does for the band. Roger Waters wrote a series of songs about mundane, everyday details which aren&#39;t that impressive by themselves, but when given the sonic backdrop of Floyd&#39;s slow, atmospheric soundscapes and carefully placed sound effects, they achieve an emotional resonance. But what gives the album true power is the subtly textured music, which evolves from ponderous, neo-psychedelic art rock to jazz fusion and blues-rock before turning back to psychedelia. It&#39;s dense with detail, but leisurely paced, creating its own dark, haunting world. Pink Floyd may have better albums than Dark Side of the Moon, but no other record defines them quite as well as this one.<br \/>\r\n<br \/>\r\n<span style=\"font-style: italic;\">AllMusic Review by Stephen Thomas Erlewine<\/span><\/blockquote>","wikiImage":"https:\/\/ptpimg.me\/yh5fqd.jpg","id":1,"name":"The Dark Side of the Moon","year":1973,"recordLabel":"","catalogueNumber":"","releaseType":1,"categoryId":1,"categoryName":"Music","time":"2019-08-28 17:46:53","vanityHouse":false,"isBookmarked":false,"musicInfo":{"composers":[],"dj":[],"artists":[{"id":1,"name":"Pink Floyd"}],"with":[],"conductor":[],"remixedBy":[],"producer":[]},"tags":["rock","experimental","progressive.rock","psychedelic","psychedelic.rock","space.rock","classic.rock","hard.rock","1970s","art.rock","british","staff.recs"]},"torrent":{"id":1,"infoHash":"C380B62A3EC6658597C56F45D596E8081B3F7A5C","media":"CD","format":"FLAC","encoding":"Lossless","remastered":true,"remasterYear":1988,"remasterTitle":"Japan MFSL UltraDisc #1, 24 Karat Gold","remasterRecordLabel":"Mobile Fidelity Sound Lab","remasterCatalogueNumber":"UDCD 517","scene":false,"hasLog":true,"hasCue":true,"logScore":70,"fileCount":12,"size":219114079,"seeders":100,"leechers":0,"snatched":414,"freeTorrent":false,"reported":false,"time":"2016-11-24 01:34:03","description":"[important]Staff: Technically trumped because EAC 0.95 logs are terrible. There is historic and sentimental value in keeping the first torrent ever uploaded to the site as well as a perfect modern rip. Take no action.[\/important]","fileList":"01 - Speak to Me.flac{{{3732587}}}|||02 -  Breathe.flac{{{14244409}}}|||03 - On the Run.flac{{{16541873}}}|||04 - Time.flac{{{35907465}}}|||05 -  The Great Gig in the Sky.flac{{{20671913}}}|||06 - Money.flac{{{37956922}}}|||07 -Us and Them.flac{{{39706774}}}|||08 - Any Colour You Like.flac{{{18736396}}}|||09 - Brain Damage.flac{{{20457034}}}|||10 - Eclipse.flac{{{11153655}}}|||Pink Floyd - Dark Side of the Moon.CUE{{{1435}}}|||Pink Floyd - Dark Side of the Moon.log{{{3616}}}","filePath":"Pink Floyd - Dark Side of the Moon (OMR MFSL 24k Gold Ultradisc II) fixed tags","userId":9,"username":"danger"}}}`
