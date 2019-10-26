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
	"fmt"
	"os"
	"reflect"
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
