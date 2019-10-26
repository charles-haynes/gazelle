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

func TestArtistUpdates(t *testing.T) {
	a := gazelle.Artist{
		Name: "foo",
		ID:   2717,
	}
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
			"Artist": {
				{Name: "foo", ID: 2717},
				{Name: "bar", ID: 3414},
			},
		},
	}
	res := a.Names()
	if !reflect.DeepEqual(res, []string{"foo", "bar"}) {
		t.Errorf(`expected {"foo", "bar"} got %#v`, res)
	}
}

func TestGetArtists(t *testing.T) {
	to := gazelle.Torrent{
		Group: gazelle.Group{
			Artists: gazelle.Artists{
				Tracker: gazelle.Tracker{Name: "bar"},
			},
			ID: 2717,
		},
	}
	expectedArtists := map[string][]gazelle.Artist{
		"Artist": {{Name: "foo", ID: 3414}},
	}
	err := to.GetArtists(db)
	if err != nil {
		t.Error(err)
	}
	if !reflect.DeepEqual(expectedArtists, to.Artists.Artists) {
		t.Errorf("expected %v got %v", expectedArtists, to.Artists.Artists)
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

INSERT INTO artists (tracker,id,name) VALUES("bar",3414,"foo");
INSERT INTO artists_groups VALUES("bar",3414,2717,"Artist");
COMMIT;
PRAGMA foreign_keys=ON;
`)
	return err

}
