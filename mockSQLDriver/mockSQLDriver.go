package mockSQLDriver

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"reflect"
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

func (c *Calls) Reset() {
	*c = (*c)[:0]
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
	return io.EOF
}

func init() {
	sql.Register("mock", &MockDriver{})
}
