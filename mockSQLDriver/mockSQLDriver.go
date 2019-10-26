package mockSQLDriver

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"reflect"
	"runtime"
)

type Call struct {
	name string
	args interface{}
}

type CallLog []Call

var Calls = CallLog{}

func (c CallLog) Append(args interface{}) {
	rpc := make([]uintptr, 1)
	runtime.Callers(2, rpc)
	frame, _ := runtime.CallersFrames(rpc).Next()
	Calls = append(Calls, Call{frame.Function, args})
}

func (c CallLog) Contains(name string, args interface{}) bool {
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

func (c CallLog) Print() {
	for _, v := range c {
		fmt.Printf("%s %#v\n", v.name, v.args)
	}
}

func (c *CallLog) Reset() {
	*c = (*c)[:0]
}

type MockDriver struct{}

func (d *MockDriver) Open(Name string) (driver.Conn, error) {
	Calls.Append(Name)
	return MockConn{}, nil
}

type MockConn struct{}

func (c MockConn) Begin() (driver.Tx, error) {
	Calls.Append(nil)
	return MockTx{}, nil
}
func (c MockConn) Close() error {
	Calls.Append(nil)
	return nil
}
func (c MockConn) Prepare(query string) (driver.Stmt, error) {
	Calls.Append(query)
	return MockStmt{}, nil
}

type MockTx struct{}

func (t MockTx) Commit() error {
	Calls.Append(nil)
	return nil
}
func (t MockTx) Rollback() error {
	Calls.Append(nil)
	return nil
}

type MockStmt struct{}

func (s MockStmt) Close() error {
	Calls.Append(nil)
	return nil
}
func (s MockStmt) NumInput() int {
	Calls.Append(nil)
	return -1
}
func (s MockStmt) Exec(args []driver.Value) (driver.Result, error) {
	Calls.Append(args)
	return driver.ResultNoRows, nil
}
func (s MockStmt) Query(args []driver.Value) (driver.Rows, error) {
	Calls.Append(args)
	return &MockRows{}, nil
}

type MockRows struct{}

func (r MockRows) Columns() []string {
	Calls.Append(nil)
	return []string{}
}
func (r MockRows) Close() error {
	Calls.Append(nil)
	return nil
}
func (r MockRows) Next(dest []driver.Value) error {
	Calls.Append(dest)
	return io.EOF
}

func init() {
	sql.Register("mock", &MockDriver{})
}
