module github.com/charles-haynes/gazelle

go 1.13

replace github.com/charles-haynes/discogs => ../discogs

replace github.com/charles-haynes/whatapi => ../whatapi

require (
	github.com/charles-haynes/discogs v0.0.0
	github.com/charles-haynes/munkres v0.0.0-20191008174651-55d467190535
	github.com/charles-haynes/strsim v0.0.0-20191011181331-ef9ead4980ee
	github.com/charles-haynes/whatapi v0.0.14
	github.com/jmoiron/sqlx v1.2.0
	github.com/mattn/go-sqlite3 v1.11.0
	golang.org/x/lint v0.0.0-20190930215403-16217165b5de // indirect
	golang.org/x/tools v0.0.0-20191101200257-8dbcdeb83d3f // indirect
)
