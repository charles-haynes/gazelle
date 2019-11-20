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
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/charles-haynes/munkres"
	"github.com/charles-haynes/strsim"
	"github.com/charles-haynes/whatapi"
	"github.com/jmoiron/sqlx"
)

// Tracker represents a gazelle tracker and implements the WhatAPI interface
type Tracker struct {
	whatapi.WhatAPI
	Name            string `db:"tracker"`
	Other           string
	Path            string
	Host            string
	Conf            string
	ReleaseTypes    map[int]string
	Categories      map[int]string
	TokenSize       int64 // torrents bigger than this, try to use a token
	updatedArtists  map[int]struct{}
	updatedGroups   map[int]struct{}
	updatedTorrents map[int]struct{}
}

// WeightedScore lets you compute a single weighted score from a series
// of scores and weights. It uses the geometric mean (distance) as the Score
type WeightedScore struct {
	s float64
	w float64
}

// Update adds a score and weight to the WeightedScore
func (s *WeightedScore) Update(sc, w float64) {
	s.s += sc * w * sc * w
	s.w += w * w
}

// Score is the current value of the WeightedScore
func (s WeightedScore) Score() float64 {
	if s.w == 0.0 {
		return 0.0
	}
	return math.Sqrt(s.s) / math.Sqrt(s.w)
}

// Similarity measures how similar two strings are from [0.0..1.0] where
// 0.0 is no similarity at all and 1.0 is equal strings
func Similarity(a, b string) float64 {
	return strsim.WrapNoCase(strsim.LCS)(a, b)
}

func scoreSize(t1, t2 Torrent) float64 {
	if len(t1.Files) == len(t2.Files) && t1.Size == t2.Size {
		return 1.0
	}
	return 0.0
}

// FileSimilarity measures how similar the files are between two torrents
// with 0.0 being not at all similar, and 1.0 being identical
func FileSimilarity(t1, t2 Torrent) (float64, error) {
	if scoreSize(t1, t2) == 1.0 {
		return 1.0, nil
	}
	if len(t1.Files) < len(t2.Files) {
		t1, t2 = t2, t1
	}
	var costs = make([][]float64, len(t1.Files))
	for i, f1 := range t1.Files {
		costs[i] = make([]float64, len(t2.Files))
		for j, f2 := range t2.Files {
			costs[i][j] = Similarity(f1.Name(), f2.Name())
		}
	}
	m, err := munkres.NewHungarianAlgorithm(costs)
	if err != nil {
		return 0.0, err
	}
	a := m.Execute()
	sumOfSquares := 0.0
	sum := 0.0
	for i, f1 := range t1.Files {
		if a[i] < 0 {
			sumOfSquares += 1.0
			sum += float64(f1.Size)
		} else {
			n := float64(f1.Size - t2.Files[a[i]].Size)
			d := float64(f1.Size + t2.Files[a[i]].Size)
			sumOfSquares += (n / d) * (n / d)
			sum += d
		}
	}
	return 1.0 - math.Sqrt(sumOfSquares/(sum*sum)), nil
}

func threeWayYear(y1, y2 int) float64 {
	if y1 == y2 {
		return 1.0
	}
	if y1 == 0 || y2 == 0 {
		return 0.5
	}
	switch y1 - y2 {
	case 0:
		return 1.0
	case 1, -1:
		return 0.9
	case 2, -2:
		return 0.6
	case 3, -3:
		return 0.1
	}
	return 0.0
}

func threeWaySim(s1, s2 string) float64 {
	if s1 == s2 {
		return 1.0
	}
	if s1 != "" && s2 != "" {
		return Similarity(s1, s2)
	}
	// one known, one unknown
	return 0.5
}

// ReleaseSimilarity measures how similar two torrent releases are
// with 0.0 being not at all similar, and 1.0 being identical
func ReleaseSimilarity(t1, t2 Torrent) float64 {
	score := WeightedScore{}
	if t1.Format != t2.Format {
		return 0.0
	}
	if t1.Encoding != t2.Encoding {
		return 0.0
	}
	if t1.Media != t2.Media {
		return 0.0
	}
	tYear := t1.Year
	tRecLab := NullableString(t1.RecordLabel)
	tCatNum := NullableString(t1.CatalogueNumber)
	tRemTit := ""
	if t1.Remastered {
		tYear = t1.RemasterYear
		tRecLab = t1.RemasterRecordLabel
		tCatNum = NullableString(t1.RemasterCatalogueNumber)
		tRemTit = t1.RemasterTitle
	}
	rYear := t2.Year
	rRecLab := NullableString(t2.RecordLabel)
	rCatNum := NullableString(t2.CatalogueNumber)
	rRemTit := ""
	if t2.Remastered {
		rYear = t2.RemasterYear
		rRecLab = t2.RemasterRecordLabel
		rCatNum = NullableString(t2.RemasterCatalogueNumber)
		rRemTit = t2.RemasterTitle
	}

	if rCatNum == "" {
		// some people have put both record label and catalogue number
		// into the record label separated by " / "
		ss := strings.SplitN(rRecLab, " / ", 2)
		if len(ss) == 2 {
			rRecLab = ss[0]
			rCatNum = ss[1]
		}
	}

	score.Update(threeWaySim(tCatNum, rCatNum), 0.50)
	score.Update(threeWaySim(tRecLab, rRecLab), 0.20)
	score.Update(threeWaySim(tRemTit, rRemTit), 0.10)
	score.Update(threeWaySim(t1.ReleaseType(), t2.ReleaseType()), 0.05)
	score.Update(threeWayYear(tYear, rYear), 0.02)

	return score.Score()
}

// Artist is a single gazelle artist
type Artist struct {
	ID   int    `db:"id"`
	Name string `db:"name"`
}

// Update artist stores the artist in the db
func (a Artist) Update(tx *sqlx.Tx, tracker Tracker) error {
	if tracker.updatedArtists == nil {
		tracker.updatedArtists = map[int]struct{}{}
	}
	if _, ok := tracker.updatedArtists[a.ID]; ok {
		return nil
	}
	_, err := tx.Exec(
		`INSERT OR REPLACE INTO artists (tracker,id,name) VALUES (?,?,?)`,
		tracker.Name, a.ID, a.Name)
	if err != nil {
		return err
	}
	tracker.updatedArtists[a.ID] = struct{}{}
	return nil
}

// Similarity measures how similar two artists are with 0.0 being not at all
// and 1.0 being the same
func (a Artist) Similarity(a2 Artist) float64 {
	if a.ID == a2.ID {
		return 1.0
	}
	return Similarity(a.Name, a2.Name)
}

// ArtistList is just a list of Artist
type ArtistList []Artist

// Names returns the names of all the artists in the list
func (a ArtistList) Names() []string {
	s := make([]string, len(a))
	for i := range a {
		s[i] = a[i].Name
	}
	return s
}

// Artists is a tracker and a set of artists and their roles
// usually associated with a group
type Artists struct {
	Tracker
	Artists map[string]ArtistList
}

// Names returns a list of the main artists names from an artist list
func (a Artists) Names() []string {
	return a.Artists["Artist"].Names()
}

// Similarity returns how similar two sets of artists are
// with 0.0 being not at all similar and 1.0 being completely the same
func (a Artists) Similarity(a2 Artists) float64 {
	var ws WeightedScore
	for c := range a.Artists {
		ws.Update(strsim.ListSimilarity(
			a.Artists[c].Names(), a2.Artists[c].Names(),
			Similarity), 1.0)
	}
	return ws.Score()
}

// GetArtists gets all of the artists for a torrent. Usually used when initially
// populating a torrent (and should probably just be part of get torrent)
func (t *Torrent) GetArtists(db *sqlx.DB) error {
	var artists []struct {
		ID   int    `db:"id"`
		Name string `db:"name"`
		Role string `db:"role"`
	}
	err := db.Select(&artists, `
SELECT ga.id, ga.name, gag.role
FROM artists_groups AS gag
JOIN artists AS ga ON gag.tracker=ga.tracker AND gag.artistid=ga.id
WHERE gag.tracker=? AND gag.groupid=?`, t.Tracker.Name, t.Group.ID)
	if err != nil {
		return err
	}
	if t.Artists.Artists == nil {
		t.Artists.Artists = map[string]ArtistList{}
	}
	for _, a := range artists {
		t.Artists.Artists[a.Role] = append(
			t.Artists.Artists[a.Role], Artist{a.ID, a.Name})
	}
	return nil
}

// DisplayName for an artist list is the human readable form of the
// list, or "Various <role>" if multiple
func (a ArtistList) DisplayName(role string) string {
	switch len(a) {
	case 0:
		return ""
	case 1:
		return a[0].Name
	case 2:
		return a[0].Name + " & " + a[1].Name
	default:
		return "Various " + role
	}
}

// DisplayName for artists is the human readable string formatting of the
// artists, intended to replicate the internal gazelle logic for formatting
// artist names
func (a Artists) DisplayName() string {
	return a.Artists["Artist"].DisplayName("Artists")
}

// Update artists just updates each artist in the aggregate
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

// NewMusicInfo creates Artists from a tracker and whatapi.MusicInfo struct
func NewMusicInfo(tracker Tracker, mi whatapi.MusicInfo) Artists {
	artists := map[string]ArtistList{}
	artists["Composer"] = make([]Artist, len(mi.Composers))
	for i, m := range mi.Composers {
		artists["Composer"][i] = Artist{m.ID, m.Name}
	}
	artists["DJ"] = make([]Artist, len(mi.DJ))
	for i, m := range mi.DJ {
		artists["DJ"][i] = Artist{m.ID, m.Name}
	}
	artists["Artist"] = make([]Artist, len(mi.Artists))
	for i, m := range mi.Artists {
		artists["Artist"][i] = Artist{m.ID, m.Name}
	}
	artists["With"] = make([]Artist, len(mi.With))
	for i, m := range mi.With {
		artists["With"][i] = Artist{m.ID, m.Name}
	}
	artists["Conductor"] = make([]Artist, len(mi.Conductor))
	for i, m := range mi.Conductor {
		artists["Conductor"][i] = Artist{m.ID, m.Name}
	}
	artists["RemixedBy"] = make([]Artist, len(mi.RemixedBy))
	for i, m := range mi.RemixedBy {
		artists["RemixedBy"][i] = Artist{m.ID, m.Name}
	}
	artists["Producer"] = make([]Artist, len(mi.Producer))
	for i, m := range mi.Producer {
		artists["Producer"][i] = Artist{m.ID, m.Name}
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

// NewExtendedArtistMap creates Artists from a tracker and a whatapi.ExtendedArtistMap
func NewExtendedArtistMap(tracker Tracker, am whatapi.ExtendedArtistMap) Artists {
	a := map[string]ArtistList{}
	for r, m := range am {
		a[roles[r]] = make([]Artist, len(m))
		for i, ma := range m {
			a[roles[r]][i] = Artist{
				ID:   ma.ID,
				Name: ma.Name,
			}
		}
	}
	return Artists{tracker, a}
}

type Group struct {
	Artists
	ID              int        `db:"groupid"`
	Name            string     `db:"groupname"`
	Year            int        `db:"year"`
	RecordLabel     *string    `db:"recordlabel"`
	CatalogueNumber *string    `db:"cataloguenumber"`
	ReleaseTypeF    int        `db:"releasetype"`
	CategoryID      *int       `db:"categoryid"`
	CategoryName    *string    `db:"categoryname"`
	Time            *time.Time `db:"time"`
	VanityHouse     bool       `db:"vanityhouse"`
	WikiImage       *string    `db:"wikiimage"`
	WikiBody        *string    `db:"wikibody"`
	IsBookmarked    *bool      `db:"isbookmarked"`
	Tags            string     `db:"tags"`
}

func (g Group) ReleaseType() string {
	if r, ok := g.ReleaseTypes[g.ReleaseTypeF]; ok {
		return r
	}
	return "Unknown"
}

type Torrent struct {
	Group
	ID         int     `db:"id"`
	Hash       *string `db:"hash"`
	Media      string  `db:"media"`
	Format     string  `db:"format"`
	Encoding   string  `db:"encoding"`
	Remastered bool    `db:"remastered"`

	RemasterYear            int       `db:"remasteryear"`
	RemasterTitle           string    `db:"remastertitle"`
	RemasterRecordLabel     string    `db:"remasterlabel"`
	RemasterCatalogueNumber *string   `db:"cataloguenumber"`
	Scene                   bool      `db:"scene"`
	HasLog                  bool      `db:"haslog"`
	HasCue                  bool      `db:"hascue"`
	LogScore                int       `db:"logscore"`
	LogChecksum             *bool     `db:"logchecksum"`
	FileCount               int       `db:"filecount"`
	Size                    int64     `db:"size"`
	Seeders                 int       `db:"seeders"`
	Leechers                int       `db:"leechers"`
	Snatched                int       `db:"snatched"`
	FreeTorrent             bool      `db:"freetorrent"`
	Reported                *bool     `db:"reported"`
	Time                    time.Time `db:"time"`
	Description             *string   `db:"description"`
	FilePath                *string   `db:"filepath"`
	UserID                  *int      `db:"userid"`
	Username                *string   `db:"username"`
	Files                   []whatapi.FileStruct
	CanUseToken             *bool `db:"canusetoken"`
}

func (t Torrent) ShortName() string {
	return fmt.Sprintf("%s-%d", t.Tracker.Name, t.ID)
}

func (t *Torrent) Fill(tx *sqlx.Tx) error {
	if t.Files != nil && NullableString(t.FilePath) != "" {
		return nil // already filled
	}
	start := time.Now()
	fmt.Printf("#     filling %s\n", t.ShortName())
	var err error
	var tg = make([]Torrent, 1)
	if t.Group.ID == 0 {
		tg[0], err = t.GetTorrent(t.ID)
	} else {
		tg, err = t.GetGroup(t.Group.ID)
	}
	if err != nil {
		return err
	}
	for _, ti := range tg {
		if ti.ID == t.ID {
			*t = ti
		}
		ti.Update(tx)
	}
	fmt.Printf("#     fill took %s\n", time.Since(start))
	return nil
}

func (g Group) UpdateArtistsGroups(tx *sqlx.Tx) error {
	for r, as := range g.Artists.Artists {
		for _, a := range as {
			_, err := tx.Exec(`INSERT OR REPLACE INTO artists_groups VALUES (?,?,?,?)`,
				g.Tracker.Name, a.ID, g.ID, r)
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

func (g Group) Update(tx *sqlx.Tx) error {
	if g.updatedGroups == nil {
		g.updatedGroups = map[int]struct{}{}
	}
	if _, ok := g.updatedGroups[g.ID]; ok {
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
		g.Tracker.Name,      // tracker
		g.WikiBody,          // wikibody
		g.WikiImage,         // wikiimage
		g.ID,                // id
		g.Name,              // name
		g.Year,              // year
		g.RecordLabel,       // recordlabel
		g.CatalogueNumber,   // cataloguenumber
		g.ReleaseTypeF,      // releasetype
		g.CategoryID,        // categoryid
		g.CategoryName,      // categoryname
		g.Time,              // time
		Bint(g.VanityHouse), // vanityhouse
		g.Tags,              // tags
	)
	if err != nil {
		return err
	}

	err = g.Artists.Update(tx)
	if err != nil {
		return err
	}

	err = g.UpdateArtistsGroups(tx)
	if err != nil {
		return err
	}

	g.updatedGroups[g.ID] = struct{}{}
	return nil
}

func NewGroupStruct(tracker Tracker, gs whatapi.GroupStruct) (g Group, err error) {
	al := NewMusicInfo(tracker, gs.MusicInfo)
	gtime, err := time.Parse("2006-01-02 15:04:05", gs.Time)
	if err != nil {
		return g, err
	}
	g = Group{
		Artists:         al,
		WikiImage:       &gs.WikiImageF,
		WikiBody:        &gs.WikiBodyF,
		ID:              gs.ID(),
		Name:            gs.Name(),
		Year:            gs.Year(),
		RecordLabel:     &gs.RecordLabelF,
		CatalogueNumber: &gs.CatalogueNumberF,
		ReleaseTypeF:    gs.ReleaseType(),
		CategoryID:      &gs.CategoryID,
		CategoryName:    &gs.CategoryName,
		Time:            &gtime,
		VanityHouse:     gs.VanityHouse,
		IsBookmarked:    &gs.IsBookmarked,
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
			t.Tracker.Name, t.ID, f.Name(), f.Size)
		if err != nil {
			return err
		}
	}
	return nil
}

func (t Torrent) Update(tx *sqlx.Tx) error {
	if t.updatedTorrents == nil {
		t.updatedTorrents = map[int]struct{}{}
	}
	if _, ok := t.updatedTorrents[t.ID]; ok {
		return nil
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
		t.Tracker.Name,            // tracker
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
	t.updatedTorrents[t.ID] = struct{}{}
	return nil
}

func (t *Torrent) GetFiles(db *sqlx.DB) error {
	if t.Files != nil {
		return nil
	}
	if t.FilePath != nil {
		var f []whatapi.FileStruct
		err := db.Select(&f, `
SELECT name AS namef, size
FROM files
WHERE tracker=? AND torrentid=?`, t.Tracker.Name, t.ID)
		if err != nil {
			return err
		}
		if len(f) == t.FileCount {
			t.Files = f
			return nil
		}
	}
	tx, err := db.Beginx()
	if err == nil {
		err = t.Fill(tx)
	}
	if err == nil {
		err = tx.Commit()
	}
	return err
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
	return fmt.Sprintf("%s: %s - %s (%04d) [%s %s %s]%s [%s]",
		t.ShortName(),
		strings.Join(t.Names(), ","), t.Group.Name, t.Year,
		t.Media, t.Format, t.Encoding,
		remaster, t.ReleaseType())
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

func equalArtists(a, b []whatapi.ArtistAlias) bool {
	for i, a := range a {
		if a.ID != b[i].ID ||
			a.Name != b[i].Name ||
			a.AliasID != b[i].AliasID {
			return false
		}
	}
	return true
}

func NewGroupSearchResult(tracker Tracker, srs whatapi.TorrentSearchResultStruct) (Group, error) {
	if len(srs.Torrents) == 0 {
		return Group{},
			fmt.Errorf("search result has no torrents group %d", srs.GroupID)
	}
	for i, t := range srs.Torrents {
		if !equalArtists(srs.Torrents[0].Artists, t.Artists) {
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
			ID:   a.ID,
			Name: a.Name,
		}
	}
	return Group{
		Artists: Artists{tracker, map[string]ArtistList{"Artist": al}},
		ID:      srs.GroupID,
		Name:    srs.GroupName,
		Year:    srs.GroupYear,
		// RecordLabel:
		// CatalogueNumber:
		ReleaseTypeF: srs.ReleaseTypeF,
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
		RemasterYear:  rt.RemasterYear(),
		RemasterTitle: rt.RemasterTitle(),
		// RemasterRecordLabel:     "",
		RemasterCatalogueNumber: &rt.RemasterCatalogueNumberF,
		Scene:                   rt.Scene(),
		HasLog:                  rt.HasLog(),
		HasCue:                  rt.HasCue,
		LogScore:                rt.LogScore,
		FileCount:               rt.FileCount(),
		Size:                    rt.Size,
		Seeders:                 rt.Seeders,
		Leechers:                rt.Leechers,
		Snatched:                rt.Snatches,
		FreeTorrent:             rt.IsFreeleech || rt.IsPersonalFreeleech,
		// Reported:                nil,
		Time: tTime,
		// Description:             nil,
		// FilePath:                nil,
		// UserID:                  nil,
		// Username:                nil,
		// Files:                   nil,
		CanUseToken: &rt.CanUseToken,
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
	torrents = []Torrent{}
	for i, ag := range a.TorrentGroup {
		al := NewExtendedArtistMap(tracker, ag.ExtendedArtists)
		categoryID, err := strconv.Atoi(ag.GroupCategoryID)
		if err != nil {
			return torrents, err
		}
		g := Group{
			Artists:         al,
			ID:              ag.GroupID,
			Name:            ag.GroupName(),
			Year:            ag.Year(),
			RecordLabel:     &a.TorrentGroup[i].GroupRecordLabelF,
			CatalogueNumber: &a.TorrentGroup[i].GroupCatalogueNumberF,
			ReleaseTypeF:    ag.ReleaseTypeF,
			CategoryID:      &categoryID,
			// CategoryName
			// Time
			VanityHouse: ag.GroupVanityHouse,
			WikiImage:   &a.TorrentGroup[i].WikiImage,
			// WikiBody:        nil,
			IsBookmarked: &a.TorrentGroup[i].HasBookmarked,
			Tags:         strings.Join(ag.Tags(), ","),
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
				RemasterYear:        rt.RemasterYear(),
				RemasterTitle:       rt.RemasterTitle(),
				RemasterRecordLabel: rt.RemasterRecordLabel(),
				// RemasterCatalogueNumber: nil,
				Scene:       rt.Scene(),
				HasLog:      rt.HasLog(),
				HasCue:      rt.HasCue,
				LogScore:    rt.LogScore,
				FileCount:   rt.FileCount(),
				Size:        rt.Size,
				Seeders:     rt.Seeders,
				Leechers:    rt.Leechers,
				Snatched:    rt.Snatched,
				FreeTorrent: rt.FreeTorrent,
				// Reported:    nil,
				Time: tTime,
				// Description: nil,
				// FilePath:    nil,
				// UserID:      nil,
				// Username:    nil,
				// Files:       nil,
			})
		}
	}
	return torrents, err
}

func (t Torrent) UpdateCross(tx *sqlx.Tx, dst Torrent) error {
	if dst.ID == 0 {
		_, err := tx.Exec(`
INSERT INTO crosses
VALUES(?,?,NULL,NULL,datetime("now"))
ON CONFLICT (tracker, torrentid) DO NOTHING`,
			t.Tracker.Name, t.ID)
		return err
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
		t.Tracker.Name, t.ID, dst.Tracker.Name, dst.ID,
		dst.Tracker.Name, dst.ID, t.Tracker.Name, t.ID)
	return err
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
	var filePath *string = &t.FilePathF
	if t.FilePath() != t.FilePathF {
		f := t.FilePath()
		filePath = &f
	}
	return Torrent{
		Group:                   g,
		ID:                      t.ID(),
		Hash:                    &t.InfoHash,
		Media:                   t.Media(),
		Format:                  t.Format(),
		Encoding:                t.Encoding(),
		Remastered:              t.Remastered(),
		RemasterYear:            t.RemasterYear(),
		RemasterTitle:           t.RemasterTitle(),
		RemasterRecordLabel:     t.RemasterRecordLabel(),
		RemasterCatalogueNumber: &t.RemasterCatalogueNumberF,
		Scene:                   t.Scene(),
		HasLog:                  t.HasLog(),
		HasCue:                  t.HasCue,
		LogScore:                t.LogScore,
		FileCount:               t.FileCount(),
		Size:                    t.Size,
		Seeders:                 t.Seeders,
		Leechers:                t.Leechers,
		Snatched:                t.Snatched,
		FreeTorrent:             t.FreeTorrent,
		Reported:                &t.Reported,
		Time:                    ttime,
		Description:             &t.DescriptionF,
		FilePath:                filePath,
		UserID:                  &t.UserID,
		Username:                &t.Username,
		Files:                   files,
		// LogChecksum
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
		for i, r := range t.Results {
			if _, ok := res[r.TorrentID]; ok {
				continue
			}
			a := Artists{
				Tracker: tracker,
				Artists: map[string]ArtistList{
					"Artist": {{Name: r.Artist}},
				},
			}
			rt, err := strconv.Atoi(r.ReleaseType)
			if err != nil {
				return nil, fmt.Errorf("releaseType: %w", err)
			}
			categoryID := r.GroupCategory
			g := Group{
				Artists:      a,
				ID:           r.GroupID,
				Name:         r.GroupName,
				CategoryID:   &categoryID,
				Year:         r.GroupYear,
				Tags:         strings.Join(r.Tags, ","),
				WikiImage:    &t.Results[i].WikiImage,
				ReleaseTypeF: rt,
			}
			logScore, err := strconv.Atoi(r.LogScore)
			if err != nil {
				return nil, fmt.Errorf("LogScore: %w", err)
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
				RemasterYear:  r.Year,
				RemasterTitle: r.RemasterTitle(),
				// RemasterRecordLabel
				// RemasterCatalogueNumber
				Scene:       r.Scene,
				HasLog:      r.HasLog,
				HasCue:      r.HasCue,
				LogScore:    logScore,
				LogChecksum: &logChecksum,
				// FileCount
				Size:     r.Size,
				Seeders:  r.Seeders,
				Leechers: r.Leechers,
				Snatched: r.Snatched,
				// FreeTorrent
				// Reported
				// Time
				// Description
				// FilePath
				// UserID
				// UserName
				// Files
				// r.Data
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
