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
	"database/sql/driver"
	"reflect"
	"testing"

	"github.com/charles-haynes/gazelle"
	"github.com/charles-haynes/gazelle/mockSQLDriver"
	"github.com/jmoiron/sqlx"
)

func TestArtistUpdates(t *testing.T) {
	mockSQLDriver.Calls.Reset()
	a := gazelle.Artist{
		Name: "foo",
		ID:   2717,
	}
	db, err := sqlx.Connect("mock", "")
	if err != nil {
		t.Errorf("%s", err)
	}
	defer db.Close()
	tx, err := db.Beginx()
	if err != nil {
		t.Errorf("%s", err)
	}
	defer tx.Rollback()
	tracker := gazelle.Tracker{Name: "bar"}
	err = a.Update(tx, tracker)
	if err != nil {
		t.Errorf("%s", err)
	}
	if !mockSQLDriver.Calls.Contains(
		"stmt.Exec", []driver.Value{"bar", int64(2717), "foo"}) {
		t.Errorf(`expected to exec with "bar", 2717, "foo"`)
	}
}

func TestArtistsNames(t *testing.T) {
	mockSQLDriver.Calls.Reset()
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
	mockSQLDriver.Calls.Reset()
	db, err := sqlx.Connect("mock", "")
	if err != nil {
		t.Errorf("%s", err)
	}
	defer db.Close()
	to := gazelle.Torrent{
		Group: gazelle.Group{
			Artists: gazelle.Artists{
				Tracker: gazelle.Tracker{Name: "bar"},
			},
			ID: 2717,
		},
	}
	// mock return value from query
	to.GetArtists(db)
	if !mockSQLDriver.Calls.Contains(
		"stmt.Query", []driver.Value{"bar", int64(2717)}) {
		t.Errorf(`expected to query with "bar", 2717`)
	}
}
