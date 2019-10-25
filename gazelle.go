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

package gazelle

import (
	"fmt"
	"html"
	"net/url"
	"os"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/charles-haynes/whatapi"
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

type Tracker struct {
	whatapi.WhatAPI
	name         string
	other        string
	path         string
	host         string
	conf         string
	releaseTypes map[int64]string
	categories   []string
	tokenSize    int64 // torrents bigger than this, try to use a token
}

type Artist struct {
	ID   int64  `db:"id"`
	Name string `db:"name"`
}

var updatedArtists = map[string]map[int64]struct{}{}

func (a Artist) Update(tx *sqlx.Tx, tracker Tracker) error {
	if _, ok := updatedArtists[tracker.name]; ok {
		if _, ok := updatedArtists[tracker.name][a.ID]; ok {
			return nil
		}
	} else {
		updatedArtists[tracker.name] = map[int64]struct{}{}
	}
	_, err := tx.Exec(
		`INSERT OR REPLACE INTO artists (tracker,id,name) VALUES (?,?,?)`,
		tracker.name, a.ID, a.Name)
	if err != nil {
		return err
	}
	updatedArtists[tracker.name][a.ID] = struct{}{}
	return nil
}

type Artists struct {
	Tracker
	Artists map[string][]Artist
}

func (a Artists) Names() []string {
	s := make([]string, len(a.Artists["Artist"]))
	for i, a := range a.Artists["Artist"] {
		s[i] = a.Name
	}
	return s
}

func (a Artists) DisplayName() string {
	switch len(a.Artists["Artist"]) {
	case 0:
		return ""
	case 1:
		return a.Artists["Artist"][0].Name
	case 2:
		return a.Artists["Artist"][0].Name + " & " +
			a.Artists["Artist"][1].Name
	default:
		return "Various Artists"
	}
}

func (a Artists) Update(tx *sqlx.Tx) error {
	for _, ar := range a.Artists {
		for _, as := range ar {
			if err := as.Update(tx, a.Tracker); err != nil {
				return err
			}
		}
	}
	return nil
}

func NewMusicInfo(tracker Tracker, mi whatapi.MusicInfo) Artists {
	artists := map[string][]Artist{}
	artists["Composer"] = make([]Artist, len(mi.Composers))
	for i, m := range mi.Composers {
		artists["Composer"][i] = Artist{int64(m.ID), m.Name}
	}
	artists["DJ"] = make([]Artist, len(mi.DJ))
	for i, m := range mi.DJ {
		artists["DJ"][i] = Artist{int64(m.ID), m.Name}
	}
	artists["Artist"] = make([]Artist, len(mi.Artists))
	for i, m := range mi.Artists {
		artists["Artist"][i] = Artist{int64(m.ID), m.Name}
	}
	artists["With"] = make([]Artist, len(mi.With))
	for i, m := range mi.With {
		artists["With"][i] = Artist{int64(m.ID), m.Name}
	}
	artists["Conductor"] = make([]Artist, len(mi.Conductor))
	for i, m := range mi.Conductor {
		artists["Conductor"][i] = Artist{int64(m.ID), m.Name}
	}
	artists["RemixedBy"] = make([]Artist, len(mi.RemixedBy))
	for i, m := range mi.RemixedBy {
		artists["RemixedBy"][i] = Artist{int64(m.ID), m.Name}
	}
	artists["Producer"] = make([]Artist, len(mi.Producer))
	for i, m := range mi.Producer {
		artists["Producer"][i] = Artist{int64(m.ID), m.Name}
	}
	return Artists{
		Tracker: tracker,
		Artists: artists,
	}
}

var roles = map[string]string{
	"1":         "Artist",
	"2":         "With",
	"3":         "RemixedBy",
	"4":         "Composer",
	"5":         "Conductor",
	"6":         "DJ",
	"7":         "Producer",
	"Artist":    "Artist",
	"With":      "With",
	"RemixedBy": "RemixedBy",
	"Composer":  "Composer",
	"Conductor": "Conductor",
	"DJ":        "DJ",
	"Producer":  "Producer",
}

func NewExtendedArtistMap(tracker Tracker, am whatapi.ExtendedArtistMap) Artists {
	a := map[string][]Artist{}
	for r, m := range am {
		a[roles[r]] = make([]Artist, len(m))
		for i, ma := range m {
			a[roles[r]][i] = Artist{
				ID:   int64(ma.ID),
				Name: ma.Name,
			}
		}
	}
	return Artists{tracker, a}
}

type Group struct {
	Artists
	ID              int64     `db:"groupid"`
	Name            string    `db:"groupname"`
	Year            int64     `db:"year"`
	RecordLabel     string    `db:"recordlabel"`
	CatalogueNumber *string   `db:"cataloguenumber"`
	ReleaseTypeF    int64     `db:"releasetype"`
	CategoryID      int64     `db:"categoryid"`
	CategoryName    string    `db:"categorynamee"`
	Time            time.Time `db:"time"`
	VanityHouse     bool      `db:"vanityhouse"`
	WikiImage       *string   `db:"wikiimage"`
	WikiBody        *string   `db:"wikibody"`
	Tags            string    `db:"tags"`
}

func (g Group) ReleaseType() string {
	if r, ok := g.releaseTypes[g.ReleaseTypeF]; ok {
		return r
	}
	return "Unknown"
}

type Torrent struct {
	Group
	ID                      int       `db:"id"`
	Hash                    *string   `db:"hash"`
	Media                   string    `db:"media"`
	Format                  string    `db:"format"`
	Encoding                string    `db:"encoding"`
	Remastered              bool      `db:"remastered"`
	RemasterYear            int64     `db:"remasteryear"`
	RemasterTitle           string    `db:"remastertitle"`
	RemasterRecordLabel     string    `db:"remasterlabel"`
	RemasterCatalogueNumber *string   `db:"cataloguenumber"`
	Scene                   bool      `db:"scene"`
	HasLog                  bool      `db:"haslog"`
	HasCue                  bool      `db:"hascue"`
	LogScore                int64     `db:"logscore"`
	LogChecksum             *bool     `db:"logchecksum"`
	FileCount               int64     `db:"filecount"`
	Size                    int64     `db:"size"`
	Seeders                 int64     `db:"seeders"`
	Leechers                int64     `db:"leechers"`
	Snatched                int64     `db:"snatched"`
	FreeTorrent             bool      `db:"freetorrent"`
	Reported                *bool     `db:"reported"`
	Time                    time.Time `db:"time"`
	Description             *string   `db:"description"`
	FilePath                *string   `db:"filepath"`
	UserID                  *int      `db:"userid"`
	Username                *string   `db:"username"`
	Files                   []whatapi.FileStruct
	Logs                    []string
}

func (t Torrent) ShortName() string {
	return fmt.Sprintf("%s-%d", t.Tracker, t.ID)
}

func (t *Torrent) Fill(tx *sqlx.Tx) error {
	if t.Files != nil && t.FilePath != nil {
		return nil // already filled
	}
	start := time.Now()
	fmt.Printf("#     filling %s\n", t.ShortName())
	var err error
	*t, err = t.GetTorrent(t.ID)
	if err != nil {
		return err
	}
	t.Update(tx)
	fmt.Printf("#     fill torrent took %s\n", time.Since(start))
	return nil
}

func (g Group) UpdateArtistsGroups(tx *sqlx.Tx) error {
	for r, as := range g.Artists.Artists {
		for _, a := range as {
			_, err := tx.Exec(`INSERT OR REPLACE INTO artists_groups VALUES (?,?,?,?)`,
				g.Tracker, a.ID, g.ID, r)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func Bint(b bool) int {
	if b {
		return 1
	}
	return 0
}

type grp struct {
	Tracker string
	ID      int64
}

var updatedGroups = map[grp]struct{}{}

func (g Group) Update(tx *sqlx.Tx) error {
	if _, ok := updatedGroups[grp{g.Tracker.name, g.ID}]; ok {
		return nil
	}
	_, err := tx.Exec(
		`INSERT INTO groups VALUES (`+
			`?,`+ // tracker
			`?,`+ // wikibody
			`?,`+ // wikiimage
			`?,`+ // id
			`?,`+ // name
			`?,`+ // year
			`?,`+ // recordlabel
			`?,`+ // cataloguenumber
			`?,`+ // releasetype
			`?,`+ // categoryid
			`?,`+ // categoryname
			`?,`+ // time
			`?,`+ // vanityhouse
			`null,`+ // isbookmarked
			`?`+ // tags
			`) ON CONFLICT (tracker,id) DO UPDATE SET `+
			`wikibody=excluded.wikibody,`+
			`wikiimage=excluded.wikiimage,`+
			`categoryid=excluded.categoryid,`+
			`categoryname=excluded.categoryname,`+
			`time=excluded.time`,
		g.Tracker.name,      // tracker
		g.WikiBody,          // wikibody
		g.WikiImage,         // wikiimage
		g.ID,                // id
		g.Name,              // name
		g.Year,              // year
		g.RecordLabel,       // recordlabel
		g.CatalogueNumber,   // cataloguenumber
		g.ReleaseType(),     // releasetype (like "Album")
		g.CategoryID,        // categoryid
		g.CategoryName,      // categoryname
		g.Time,              // time
		Bint(g.VanityHouse), // vanityhouse
		g.Tags,              // tags
	)
	if err != nil {
		return err
	}

	err = g.UpdateArtistsGroups(tx)
	if err != nil {
		return err
	}

	err = g.Artists.Update(tx)
	if err != nil {
		return err
	}

	updatedGroups[grp{g.Tracker.name, g.ID}] = struct{}{}
	return nil
}

func NewGroupStruct(tracker Tracker, gs whatapi.GroupStruct) (g Group, err error) {
	al := NewMusicInfo(tracker, gs.MusicInfo)
	gtime, err := time.Parse("2006-01-02 15:04:05", gs.Time)
	return g, err
	g = Group{
		Artists:         al,
		WikiImage:       &gs.WikiImageF,
		WikiBody:        &gs.WikiBodyF,
		ID:              int64(gs.ID()),
		Name:            gs.Name(),
		Year:            int64(gs.Year()),
		RecordLabel:     gs.RecordLabel(),
		CatalogueNumber: &gs.CatalogueNumberF,
		ReleaseTypeF:    int64(gs.ReleaseType()),
		CategoryID:      int64(gs.CategoryID),
		CategoryName:    gs.CategoryName,
		Time:            gtime,
		VanityHouse:     gs.VanityHouse,
		Tags:            strings.Join(gs.Tags(), ","),
	}
	return g, nil
}

func (t Torrent) UpdateFiles(tx *sqlx.Tx) error {
	for _, f := range t.Files {
		_, err := tx.Exec(`INSERT OR IGNORE INTO files VALUES(`+
			`?,`+ // tracker
			`?,`+ // torrentid
			`?,`+ // name
			`?)`, // size
			t.Tracker.name, t.ID, f.Name(), f.Size)
		if err != nil {
			return err
		}
	}
	return nil
}

var updatedTorrents = map[string]map[int]struct{}{}

func (t Torrent) Update(tx *sqlx.Tx) error {
	if _, ok := updatedTorrents[t.Tracker.name]; ok {
		if _, ok := updatedTorrents[t.Tracker.name][t.ID]; ok {
			return nil
		}
	} else {
		updatedTorrents[t.Tracker.name] = map[int]struct{}{}
	}
	if err := t.Group.Update(tx); err != nil {
		return err
	}
	_, err := tx.Exec(`INSERT OR IGNORE INTO torrents VALUES (`+
		`?,`+ // tracker
		`?,`+ // id
		`?,`+ // groupid
		`?,`+ // hash
		`?,`+ // media
		`?,`+ // format
		`?,`+ // encoding
		`?,`+ // remastered
		`?,`+ // remasteryear
		`?,`+ // remastertitle
		`?,`+ // remasterlabel
		`?,`+ // cataloguenumber
		`?,`+ // scene
		`?,`+ // haslog
		`?,`+ // hascue
		`?,`+ // logscore
		`?,`+ // filecount
		`?,`+ // size
		`?,`+ // seeders
		`?,`+ // leechers
		`?,`+ // snatched
		`?,`+ // freetorrent
		`?,`+ // reported
		`?,`+ // time
		`?,`+ // description
		`?,`+ // filepath
		`?,`+ // userid
		`?)`, // username
		t.Tracker.name,            // tracker
		t.ID,                      // id
		t.Group.ID,                // groupid
		t.Hash,                    // hash
		t.Media,                   // media
		t.Format,                  // format
		t.Encoding,                // encoding
		Bint(t.Remastered),        // remastered
		t.RemasterYear,            // remasteryear
		t.RemasterTitle,           // remastertitle
		t.RemasterRecordLabel,     // remasterlabel
		t.RemasterCatalogueNumber, // cataloguenumber
		Bint(t.Scene),             // scene
		Bint(t.HasLog),            // haslog
		Bint(t.HasCue),            // hascue
		t.LogScore,                // logscore
		t.FileCount,               // filecount
		t.Size,                    // size
		t.Seeders,                 // seeders
		t.Leechers,                // leechers
		t.Snatched,                // snatched
		Bint(t.FreeTorrent),       // freetorrent
		NullableBInt(t.Reported),  // reported
		t.Time,                    // time
		t.Description,             // description
		t.FilePath,                // filepath
		t.UserID,                  // userid
		t.Username)                // username
	if err != nil {
		return err
	}
	if err := t.UpdateFiles(tx); err != nil {
		return err
	}
	updatedTorrents[t.Tracker.name][t.ID] = struct{}{}
	return nil
}

var trackers = map[string]Tracker{
	"red": {
		name:  "red",
		other: "orp",
		path:  "redacted",
		host:  "harp.ceh.bz",
		conf:  "/home/haynes/.seed-red.yaml",
		releaseTypes: map[int64]string{
			1:  "Album",
			3:  "Soundtrack",
			5:  "EP",
			6:  "Anthology",
			7:  "Compilation",
			9:  "Single",
			11: "Live album",
			13: "Remix",
			14: "Bootleg",
			15: "Interview",
			16: "Mixtape",
			17: "Demo",
			18: "Concert Recording",
			19: "DJ Mix",
		},
		categories: []string{
			"",
			"Music",
			"Application",
			"E-Book",
			"Audiobook",
			"E-Learning Video",
			"Comedy",
			"Comic",
		},
		tokenSize: 500 * 1000 * 1000,
	},
	"orp": {
		name:  "orp",
		other: "red",
		path:  "orpheus",
		host:  "fife.ceh.bz",
		conf:  "/home/haynes/.seed-orp.yaml",
		releaseTypes: map[int64]string{
			1:  "Album",
			3:  "Soundtrack",
			5:  "EP",
			6:  "Anthology",
			7:  "Compilation",
			9:  "Single",
			11: "Live album",
			13: "Remix",
			14: "Bootleg",
			15: "Interview",
			16: "Mixtape",
			17: "DJ Mix",
			18: "Concert recording",
		},
		categories: []string{
			"",
			"Music",
			"Application",
			"E-Book",
			"Audiobook",
			"E-Learning Video",
			"Comedy",
			"Comic",
		},
		tokenSize: 100 * 1000 * 1000,
	},
}

func (t *Torrent) String() string {
	// TODO: use a template?
	remaster := ""
	if t.Remastered {
		remaster = fmt.Sprintf("{(%4d) %s/%s/%s}",
			t.RemasterYear, t.RemasterRecordLabel,
			NullableString(t.RemasterCatalogueNumber),
			t.RemasterTitle)
	}
	return fmt.Sprintf("%s-%d: %s - %s (%04d) [%s %s %s]%s [%s]",
		t.Tracker.name, t.ID,
		strings.Join(t.Names(), ","), t.Group.Name, t.Year,
		t.Media, t.Format, t.Encoding,
		remaster, t.ReleaseType())
}

func FatalN(err error, n int) {
	rpc := make([]uintptr, 1)
	runtime.Callers(n, rpc)
	frame, _ := runtime.CallersFrames(rpc).Next()
	fmt.Printf("FATAL %s:%d %s: %s\n",
		frame.File, frame.Line, frame.Function, err)
	os.Exit(-1)
}

func Fatal(err error) {
	FatalN(err, 3)
}

func DieIfError(err error) {
	if err != nil {
		FatalN(err, 3)
	}
}

func NullableString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func NullableBInt(b *bool) *int {
	if b == nil {
		return nil
	}
	i := Bint(*b)
	return &i
}

func NewGroupSearchResult(tracker Tracker, srs whatapi.TorrentSearchResultStruct) (Group, error) {
	if len(srs.Torrents) == 0 {
		return Group{},
			fmt.Errorf("search result has no torrents group %d", srs.GroupID)
	}
	for i, t := range srs.Torrents {
		if !reflect.DeepEqual(srs.Torrents[0].Artists, t.Artists) {
			return Group{},
				fmt.Errorf(
					"search result group %d, torrent 0 "+
						"artists != torrent %d artists",
					srs.GroupID, i)
		}
	}
	al := make([]Artist, len(srs.Torrents[0].Artists))
	for i, a := range srs.Torrents[0].Artists {
		al[i] = Artist{
			ID:   int64(a.ID),
			Name: a.Name,
		}
	}
	return Group{
		Artists: Artists{tracker, map[string][]Artist{"Artist": al}},
		ID:      int64(srs.GroupID),
		Name:    srs.GroupName,
		Year:    int64(srs.GroupYear),
		// RecordLabel:
		// CatalogueNumber:
		ReleaseTypeF: int64(srs.ReleaseTypeF),
		// CategoryID:
		// CategoryName:
		// Time:
		// VanityHouse:
		// WikiImage:
		// WikiBody:
		Tags: strings.Join(srs.Tags(), ","),
	}, nil
}

func NewSearchTorrentStruct(g Group, rt whatapi.SearchTorrentStruct) (Torrent, error) {
	tTime, err := time.Parse("2006-01-02 15:04:05", rt.Time)
	if err != nil {
		return Torrent{}, err
	}
	return Torrent{
		Group: g,
		ID:    rt.ID(),
		// Hash:          nil,
		Media:         rt.Media(),
		Format:        rt.Format(),
		Encoding:      rt.Encoding(),
		Remastered:    rt.Remastered(),
		RemasterYear:  int64(rt.RemasterYear()),
		RemasterTitle: rt.RemasterTitle(),
		// RemasterRecordLabel:     "",
		RemasterCatalogueNumber: &rt.RemasterCatalogueNumberF,
		Scene:                   rt.Scene(),
		HasLog:                  rt.HasLog(),
		HasCue:                  rt.HasCue,
		LogScore:                int64(rt.LogScore),
		FileCount:               int64(rt.FileCount()),
		Size:                    rt.Size,
		Seeders:                 int64(rt.Seeders),
		Leechers:                int64(rt.Leechers),
		// Snatched:                0,
		// FreeTorrent:             false,
		// Reported:                nil,
		Time: tTime,
		// Description:             nil,
		// FilePath:                nil,
		// UserID:                  nil,
		// Username:                nil,
		// Files:                   nil,
		// Logs:                    nil,
	}, nil
}

func NewTorrentSearch(
	tracker Tracker, ts whatapi.TorrentSearch) ([]Torrent, error) {
	torrents := []Torrent{}
	for _, srs := range ts.Results {
		g, err := NewGroupSearchResult(tracker, srs)
		if err != nil {
			return nil, err
		}
		for _, rt := range srs.Torrents {
			t, err := NewSearchTorrentStruct(g, rt)
			if err != nil {
				return nil, err
			}
			torrents = append(torrents, t)
		}
	}
	return torrents, nil
}

func NewArtist(tracker Tracker, a whatapi.Artist) (torrents []Torrent, err error) {
	for _, ag := range a.TorrentGroup {
		al := NewExtendedArtistMap(tracker, ag.ExtendedArtists)
		g := Group{
			Artists: al,
			ID:      int64(ag.GroupID),
			Name:    ag.GroupName(),
			Year:    int64(ag.Year()),
			// RecordLabel:     "",
			// CatalogueNumber: nil,
			ReleaseTypeF: int64(ag.ReleaseTypeF),
			// WikiImage:       nil,
			// WikiBody:        nil,
			Tags: strings.Join(ag.Tags(), ","),
		}
		for _, rt := range ag.Torrent {
			if rt.GroupIDF != ag.GroupID {
				return nil,
					fmt.Errorf("torrent group %d != artist group %d",
						rt.GroupIDF, ag.GroupID)
			}
			tTime, err := time.Parse("2006-01-02 15:04:05", rt.Time)
			if err != nil {
				return nil,
					fmt.Errorf("can't parse time %s", rt.Time)
			}
			torrents = append(torrents, Torrent{
				Group: g,
				ID:    rt.ID(),
				// Hash:                    nil,
				Media:               rt.Media(),
				Format:              rt.Format(),
				Encoding:            rt.Encoding(),
				Remastered:          rt.Remastered(),
				RemasterYear:        int64(rt.RemasterYear()),
				RemasterTitle:       rt.RemasterTitle(),
				RemasterRecordLabel: rt.RemasterRecordLabel(),
				// RemasterCatalogueNumber: nil,
				Scene:     rt.Scene(),
				HasLog:    rt.HasLog(),
				HasCue:    rt.HasCue,
				LogScore:  int64(rt.LogScore),
				FileCount: int64(rt.FileCount()),
				Size:      rt.Size,
				Seeders:   int64(rt.Seeders),
				Leechers:  int64(rt.Leechers),
				Snatched:  int64(rt.Snatched),
				// FreeTorrent: false,
				// Reported:    nil,
				Time: tTime,
				// Description: nil,
				// FilePath:    nil,
				// UserID:      nil,
				// Username:    nil,
				// Files:       nil,
				// Logs:        nil,
			})
		}
	}
	return torrents, err
}

func (src Torrent) UpdateCross(tx *sqlx.Tx, dst Torrent) {
	if dst.ID == 0 {
		_, err := tx.Exec(`
INSERT INTO crosses
VALUES(?,?,NULL,NULL,datetime("now"))
ON CONFLICT (tracker, torrentid) DO NOTHING`,
			src.name, src.ID)
		DieIfError(err)
		return
	}
	_, err := tx.Exec(`
INSERT INTO crosses
VALUES
(?,?,?,?,datetime("now")),
(?,?,?,?,datetime("now"))
ON CONFLICT (tracker, torrentid) DO UPDATE SET
other = excluded.other,
otherid = excluded.otherid,
time=excluded.time`,
		src.name, src.ID, dst.name, dst.ID,
		dst.name, dst.ID, src.name, src.ID)
	DieIfError(err)
}

func NewTorrentStruct(g Group, t whatapi.TorrentStruct) (Torrent, error) {
	ttime, err := time.Parse("2006-01-02 15:04:05", t.Time)
	if err != nil {
		return Torrent{}, err
	}
	files, err := t.Files()
	if err != nil {
		return Torrent{}, err
	}
	return Torrent{
		Group:                   g,
		ID:                      t.ID(),
		Hash:                    &t.InfoHash,
		Media:                   t.Media(),
		Format:                  t.Format(),
		Encoding:                t.Encoding(),
		Remastered:              t.Remastered(),
		RemasterYear:            int64(t.RemasterYear()),
		RemasterTitle:           t.RemasterTitle(),
		RemasterRecordLabel:     t.RemasterRecordLabel(),
		RemasterCatalogueNumber: &t.RemasterCatalogueNumberF,
		Scene:                   t.Scene(),
		HasLog:                  t.HasLog(),
		HasCue:                  t.HasCue,
		LogScore:                int64(t.LogScore),
		FileCount:               int64(t.FileCount()),
		Size:                    t.Size,
		Seeders:                 int64(t.Seeders),
		Leechers:                int64(t.Leechers),
		Snatched:                int64(t.Snatched),
		FreeTorrent:             t.FreeTorrent,
		Reported:                &t.Reported,
		Time:                    ttime,
		Description:             &t.DescriptionF,
		FilePath:                &t.FilePathF,
		UserID:                  &t.UserID,
		Username:                &t.Username,
		Files:                   files,
		// Logs: ...
	}, nil
}

func NewGetTorrentStruct(tracker Tracker, tr whatapi.GetTorrentStruct) (Torrent, error) {
	g, err := NewGroupStruct(tracker, tr.Group)
	if err != nil {
		return Torrent{}, err
	}
	t, err := NewTorrentStruct(g, tr.Torrent)
	return t, err
}

func (t Tracker) GetTorrent(id int) (Torrent, error) {
	tr, err := t.WhatAPI.GetTorrent(id, url.Values{})
	if err != nil {
		return Torrent{}, err
	}
	torrent, err := NewGetTorrentStruct(t, tr)
	return torrent, err
}

func (t Tracker) GetTorrentByHash(h string) (Torrent, error) {
	tr, err := t.WhatAPI.GetTorrent(0, url.Values{"hash": {h}})
	if err != nil {
		return Torrent{}, err
	}
	torrent, err := NewGetTorrentStruct(t, tr)
	return torrent, err
}

func NewTorrentGroup(tracker Tracker, tg whatapi.TorrentGroup) ([]Torrent, error) {
	g, err := NewGroupStruct(tracker, tg.Group)
	if err != nil {
		return nil, err
	}
	ts := make([]Torrent, len(tg.Torrent))
	for i, t := range tg.Torrent {
		ts[i], err = NewTorrentStruct(g, t)
		if err != nil {
			return nil, err
		}
	}
	return ts, nil
}

func (t Tracker) GetGroup(id int) ([]Torrent, error) {
	tg, err := t.GetTorrentGroup(id, url.Values{})
	if err != nil {
		return nil, err
	}
	torrents, err := NewTorrentGroup(t, tg)
	return torrents, err
}

func (t Tracker) GetGroupByHash(h string) ([]Torrent, error) {
	tg, err := t.GetTorrentGroup(0, url.Values{"hash": {h}})
	if err != nil {
		return nil, err
	}
	torrents, err := NewTorrentGroup(t, tg)
	return torrents, err
}

func (t Tracker) GetArtist(id int) ([]Torrent, error) {
	a, err := t.WhatAPI.GetArtist(id, url.Values{})
	if err != nil {
		return nil, err
	}
	torrents, err := NewArtist(t, a)
	return torrents, err
}

func (t Tracker) GetArtistByName(n string) ([]Torrent, error) {
	a, err := t.WhatAPI.GetArtist(0, url.Values{"artistname": {n}})
	if err != nil {
		return nil, err
	}
	torrents, err := NewArtist(t, a)
	return torrents, err
}

func (t Tracker) Search(params url.Values) ([]Torrent, error) {
	ts, err := t.SearchTorrents("", params)
	if err != nil {
		return nil, err
	}
	torrents, err := NewTorrentSearch(t, ts)
	return torrents, err
}

func NewTopTenTorrents(tracker Tracker, tt whatapi.TopTenTorrents) ([]Torrent, error) {
	res := map[int]Torrent{}
	for _, t := range tt {
		for _, r := range t.Results {
			if _, ok := res[r.TorrentID]; ok {
				continue
			}
			a := Artists{
				Tracker: tracker,
				Artists: map[string][]Artist{
					"Artist": {{Name: r.Artist}},
				},
			}
			g := Group{
				Artists:    a,
				ID:         int64(r.GroupID),
				Name:       r.GroupName,
				CategoryID: int64(r.GroupCategory),
				Year:       int64(r.GroupYear),
				Tags:       strings.Join(r.Tags, ","),
				WikiImage:  &r.WikiImage,
			}
			logScore, err := strconv.Atoi(r.LogScore)
			if err != nil {
				return nil, err
			}
			logChecksum := r.LogChecksum == "1"
			res[r.TorrentID] = Torrent{
				Group: g,
				ID:    r.TorrentID,
				// Hash
				Media:    r.Media,
				Format:   r.Format,
				Encoding: r.Encoding,
				// Remastered
				RemasterYear:  int64(r.Year),
				RemasterTitle: html.UnescapeString(r.RemasterTitle),
				// RemasterRecordLabel
				// RemasterCatalogueNumber
				Scene:       r.Scene,
				HasLog:      r.HasLog,
				HasCue:      r.HasCue,
				LogScore:    int64(logScore),
				LogChecksum: &logChecksum,
				// FileCount
				Size:     r.Size,
				Seeders:  int64(r.Seeders),
				Leechers: int64(r.Leechers),
				Snatched: int64(r.Snatched),
				// FreeTorrent
				// Reported
				// Time
				// Description
				// FilePAth
				// UserID
				// UserName
				// Files
				// Logs
				// r.Data
				// r.ReleaseType,
			}
		}
	}
	torrents := make([]Torrent, len(res))
	i := 0
	for _, t := range res {
		torrents[i] = t
		i++
	}
	return torrents, nil
}

func (t Tracker) Top10(params url.Values) ([]Torrent, error) {
	tt, err := t.WhatAPI.GetTopTenTorrents(params)
	if err != nil {
		return nil, err
	}
	torrents, err := NewTopTenTorrents(t, tt)
	return torrents, err
}
