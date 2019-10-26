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
	"database/sql/driver"
	"fmt"
	"reflect"
	"testing"

	"github.com/charles-haynes/gazelle"
	"github.com/jmoiron/sqlx"
)

type Call struct {
	name string
	args interface{}
}

type Calls []Call

var calls = Calls{}

func (c Calls) Add(name string, args interface{}) {
	calls = append(calls, Call{name, args})
}
func (c Calls) Contains(name string, args interface{}) bool {
	for _, v := range c {
		if v.name != name {
			continue
		}
		if reflect.DeepEqual(v.args, args) {
			return true
		}
	}
	return false
}

func (c Calls) Print() {
	for _, v := range c {
		fmt.Printf("%s %#v\n", v.name, v.args)
	}
}

type MockDriver struct{}

func (d *MockDriver) Open(Name string) (driver.Conn, error) {
	calls.Add("driver.Open", Name)
	return MockConn{}, nil
}

type MockConn struct{}

func (c MockConn) Begin() (driver.Tx, error) {
	calls.Add("conn.Begin", nil)
	return MockTx{}, nil
}
func (c MockConn) Close() error {
	calls.Add("conn.Close", nil)
	return nil
}
func (c MockConn) Prepare(query string) (driver.Stmt, error) {
	calls.Add("conn.Prepare", query)
	return MockStmt{}, nil
}

type MockTx struct{}

func (t MockTx) Commit() error {
	calls.Add("tx.Commit", nil)
	return nil
}
func (t MockTx) Rollback() error {
	calls.Add("tx.Rollback", nil)
	return nil
}

type MockStmt struct{}

func (s MockStmt) Close() error {
	calls.Add("stmt.Close", nil)
	return nil
}
func (s MockStmt) NumInput() int {
	calls.Add("stmt.NumInput", nil)
	return -1
}
func (s MockStmt) Exec(args []driver.Value) (driver.Result, error) {
	calls.Add("stmt.Exec", args)
	return driver.ResultNoRows, nil
}
func (s MockStmt) Query(args []driver.Value) (driver.Rows, error) {
	calls.Add("stmt.Query", args)
	return &MockRows{}, nil
}

type MockRows struct{}

func (r MockRows) Columns() []string {
	calls.Add("rows.Columns", nil)
	return []string{}
}
func (r MockRows) Close() error {
	calls.Add("rows.Close", nil)
	return nil
}
func (r MockRows) Next(dest []driver.Value) error {
	calls.Add("rows.Next", dest)
	return nil
}

func init() {
	sql.Register("mock", &MockDriver{})
}

func TestArtistUpdates(t *testing.T) {
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
	if !calls.Contains(
		"stmt.Exec", []driver.Value{"bar", int64(2717), "foo"}) {
		t.Errorf(`expected to exec with "bar", 2717, "foo"`)
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
	fmt.Printf("%v\n", to)
	// to.GetArtists(db)
	if !calls.Contains(
		"stmt.Exec", []driver.Value{"bar", int64(2717), "foo"}) {
		t.Errorf(`expected to exec with "bar", 2717, "foo"`)
	}
}
