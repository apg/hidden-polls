package main

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"database/sql"

	_ "github.com/lib/pq"
)

const maxIdleConns = 1
const maxOpenConns = 15

var notFound = errors.New("not found")

type poll struct {
	ID        int64
	Name      string
	IsOpen    bool
	CreatedAt time.Time
}

type choice struct {
	ID        int64
	PollID    int64
	Answer    string
	CreatedAt time.Time
}

type summary struct {
	choice
	Count      int64
	Percentage float64
}

type result struct {
	Poll      *poll
	Summaries []*summary
	Count     int64
}

type pollDALer interface {
	GetByID(pollId int64) (*poll, error)
	GetLatest() (*poll, error)
	GetChoices(pollId int64) ([]*choice, error)
	GetResults(pollId int64) (*result, error)
	Answer(pollId, choiceId int64) error
}

type pollDAL struct {
	db *sql.DB
}

func newPollDAL(db *sql.DB) pollDALer {
	return &pollDAL{db: db}
}

func (d *pollDAL) GetByID(pollId int64) (*poll, error) {
	query := `SELECT id, name, is_open, created_at FROM polls WHERE id = $1`

	rows, err := d.db.Query(query, pollId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	p := &poll{}
	if rows.Next() {
		rows.Scan(&(p.ID), &(p.Name), &(p.IsOpen), &(p.CreatedAt))
		return p, nil
	}

	return nil, notFound
}

func (d *pollDAL) GetLatest() (*poll, error) {
	query := `SELECT id, name, is_open, created_at FROM polls WHERE is_open = true ORDER BY created_at DESC LIMIT 1`

	rows, err := d.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	p := &poll{}
	if rows.Next() {
		rows.Scan(&(p.ID), &(p.Name), &(p.IsOpen), &(p.CreatedAt))
		return p, nil
	}

	return nil, notFound
}

func (d *pollDAL) GetChoices(pollId int64) ([]*choice, error) {
	query := `SELECT id, poll_id, answer, created_at FROM choices WHERE poll_id = $1 ORDER BY id`

	rows, err := d.db.Query(query, pollId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var choices []*choice

	for rows.Next() {
		c := &choice{}
		rows.Scan(&(c.ID), &(c.PollID), &(c.Answer), &(c.CreatedAt))
		choices = append(choices, c)
	}

	return choices, nil
}

func (d *pollDAL) GetResults(pollId int64) (*result, error) {
	query := `SELECT c.id, c.poll_id, c.answer, c.created_at, count(a.choice_id) FROM choices c
LEFT OUTER JOIN answers a ON a.choice_id = c.id
WHERE c.poll_id = $1
GROUP BY c.id, c.poll_id, c.answer, c.created_at, a.choice_id
ORDER BY count(a.choice_id) DESC`

	result := &result{}

	// get the poll
	p, err := d.GetByID(pollId)
	if err != nil {
		return nil, err
	}

	result.Poll = p

	rows, err := d.db.Query(query, pollId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []*summary
	var totalVotes int64

	for rows.Next() {
		s := &summary{}
		rows.Scan(&(s.ID), &(s.PollID), &(s.Answer), &(s.CreatedAt), &(s.Count))
		summaries = append(summaries, s)
		totalVotes += s.Count
	}

	if totalVotes > 0 {
		// compute percentages
		for i, s := range summaries {
			summaries[i].Percentage = float64(s.Count) / float64(totalVotes)
		}
	}
	result.Summaries = summaries
	result.Count = totalVotes

	return result, nil
}

func (d *pollDAL) Answer(pollId, choiceId int64) error {
	query := `INSERT INTO answers (choice_id, created_at)
SELECT id, NOW() FROM choices WHERE poll_id = $1 AND id = $2`

	result, err := d.db.Exec(query, pollId, choiceId)
	if err != nil {
		return nil
	}

	if rows, err := result.RowsAffected(); err != nil {
		return err
	} else if rows == 0 {
		return notFound
	}
	return nil
}

func openDB(postgres string) *sql.DB {
	if postgres == "" {
		log.Fatalf("PRIMARY_DB_URL must be set")
	}

	db, err := sql.Open("postgres", postgres)
	if err != nil {
		panic(fmt.Sprintf("Error opening postgres connection: %q", err))
	}

	db.SetMaxIdleConns(maxIdleConns)
	db.SetMaxOpenConns(maxOpenConns)

	return db
}

type app struct {
	PDAL pollDALer
}

func (a *app) Results(w http.ResponseWriter, r *http.Request) {
	// Extract the pollID, call GetResults, display it.
	pollId, err := a.getPollID(r)
	if err != nil {
		w.WriteHeader(400)
		w.Write([]byte("Bad Request"))
		return
	}

	if r.Method != "GET" {
		w.WriteHeader(405)
		w.Write([]byte("Method Not Allowed"))
		return
	}

	res, err := a.PDAL.GetResults(pollId)
	if err == notFound {
		w.WriteHeader(404)
		w.Write([]byte("Not Found"))
		return
	} else if err != nil {
		log.Printf("in=app.Results at=GetResults err=%q", err)
		w.WriteHeader(500)
		w.Write([]byte("Internal Server Error"))
		return
	}

	var buffer bytes.Buffer
	err = resultsTmpl.Execute(&buffer, res)
	if err != nil {
		log.Printf("in=app.Results at=Execute err=%q", err)
		w.WriteHeader(500)
		w.Write([]byte("Internal Server Error"))
		return
	}
	a.layout(w, res.Poll.Name, template.HTML(buffer.String()))
}

func (a *app) Answer(w http.ResponseWriter, r *http.Request) {
	// Extract the pollID, choiceID, call Answer(), redirect to Results on success. 500, or 404 otherwise.
	pollId, err := a.getPollID(r)
	if err != nil {
		w.WriteHeader(400)
		w.Write([]byte("Bad Request"))
		return
	}

	if r.Method != "POST" {
		w.WriteHeader(405)
		w.Write([]byte("Method Not Allowed"))
		return
	}

	choiceId, err := strconv.ParseInt(r.FormValue("choice_id"), 10, 64)
	if err != nil {
		w.WriteHeader(400)
		w.Write([]byte("Bad Request"))
		return
	}

	err = a.PDAL.Answer(pollId, choiceId)
	if err == notFound {
		w.WriteHeader(404)
		w.Write([]byte("Not Found"))
		return
	} else if err != nil {
		log.Printf("in=app.Results at=GetResults err=%q", err)
		w.WriteHeader(500)
		w.Write([]byte("Internal Server Error"))
		return
	}

	w.Header().Set("Location", fmt.Sprintf("/results?poll_id=%d", pollId))
	w.WriteHeader(302)
	return
}

func (a *app) Index(w http.ResponseWriter, r *http.Request) {
	pollID := int64(1)

	if r.Method != "GET" {
		w.WriteHeader(405)
		w.Write([]byte("Method Not Allowed"))
		return
	}

	p, err := a.PDAL.GetByID(1)
	if err == notFound {
		w.WriteHeader(404)
		w.Write([]byte("Not Found"))
		return
	} else if err != nil {
		log.Printf("in=app.Index at=GetLatest err=%q", err)
		w.WriteHeader(500)
		w.Write([]byte("Internal Server Error"))
		return
	}

	cs, err := a.PDAL.GetChoices(pollID)
	if err == notFound {
		w.WriteHeader(404)
		w.Write([]byte("Not Found"))
		return
	} else if err != nil {
		log.Printf("in=app.Index at=GetChoices err=%q", err)
		w.WriteHeader(500)
		w.Write([]byte("Internal Server Error"))
		return
	}

	var buffer bytes.Buffer
	err = indexTmpl.Execute(&buffer, struct {
		Poll    *poll
		Choices []*choice
	}{Poll: p, Choices: cs})

	a.layout(w, p.Name, template.HTML(buffer.String()))
}

func (a *app) getPollID(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.FormValue("poll_id"), 10, 64)
}

func (a *app) layout(w http.ResponseWriter, title string, body template.HTML) {
	var buffer bytes.Buffer
	err := layoutTmpl.Execute(&buffer, struct {
		Body  template.HTML
		Title string
	}{Body: body, Title: title})

	if err != nil {
		log.Printf("in=app.layout at=Execute err=%q", err)
		w.WriteHeader(500)
		w.Write([]byte("Internal Server Error"))
		return
	}

	w.Write(buffer.Bytes())
}

func main() {
	db := openDB(os.Getenv("DATABASE_URL"))
	dal := newPollDAL(db)
	a := &app{PDAL: dal}

	http.HandleFunc("/results", a.Results)
	http.HandleFunc("/answer", a.Answer)
	http.HandleFunc("/", a.Index)
	http.ListenAndServe(":"+os.Getenv("PORT"), nil)
}

const layoutRaw = `
<!DOCTYPE html>
<html>
	<head>
		<meta charset="UTF-8">
		<title>{{.Title}}</title>
    <link rel="stylesheet" href="//www.herokucdn.com/purple/1.0.0/purple.min.css">
    <script src="//www.herokucdn.com/purple/1.0.0/purple.min.js"></script>
	</head>
	<body>
     <div class="container">
         <header>
            <h1>Hidden Polls</h1>
         </header>

         {{.Body}}
     </div>
	</body>
</html>
`

const resultsRaw = `
<div class="row">
<h2>{{.Poll.Name}}</h2>
<p><em>{{.Count}} total votes</em></p>
<ul>
    {{range $i, $choice := .Summaries}}
    <li>{{$choice.Answer}}: {{$choice.Count}} votes ({{$choice.Percentage | printf "%.3f"}})</li>
    {{end}}
</ul>
</div>
`

const indexRaw = `
<div class="row">
<h2>{{.Poll.Name}}</h2>
<form method="POST" action="/answer">
<input type="hidden" value="{{.Poll.ID}}" name="poll_id" />
{{range $i, $choice := .Choices}}
  <p><input name="choice_id" type="radio" value="{{ $choice.ID }}" /> {{$choice.Answer}}</p>
{{end}}
<p><input type="submit" value="Vote" /></p>
</form>
</div>
`

var layoutTmpl *template.Template
var resultsTmpl *template.Template
var indexTmpl *template.Template

func init() {
	layoutTmpl = template.Must(template.New("layout").Parse(layoutRaw))
	resultsTmpl = template.Must(template.New("results").Parse(resultsRaw))
	indexTmpl = template.Must(template.New("index").Parse(indexRaw))
}
