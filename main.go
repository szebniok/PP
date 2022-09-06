package main

import (
	"database/sql"
	"fmt"
	"html/template"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	_ "github.com/mattn/go-sqlite3"
	"github.com/unrolled/render"
)

type TemplateContext struct {
	Id             int
	AddressFrom    string
	AddressTo      string
	Date           string
	Subject        string
	Text           string
	Html           string
	UnescapedHtml  template.HTML
	Categories     []string
	UnlabeledCount int
	LabeledCount   int
	IgnoredCount   int
	TotalCount     int
}

func getRandomUnlabeledId(db *sql.DB) (int, error) {
	var id int
	const query string = "SELECT id FROM unlabeled WHERE processed = FALSE AND ignored = FALSE ORDER BY RANDOM() LIMIT 1"
	if db.QueryRow(query).Scan(&id) == sql.ErrNoRows {
		return -1, fmt.Errorf("No unlabeled rows")
	} else {
		return id, nil
	}
}

func getTemplateContext(db *sql.DB, id int) (TemplateContext, error) {
	const mail_query string = `
	SELECT 
		address_from,
		address_to,
		date,
		subject,
		text,
		html
	FROM unlabeled
	WHERE id = ? AND processed = FALSE AND ignored = FALSE`

	const stats_query string = `
	SELECT todo.c, done.c, ignored.c, todo.c + done.c + ignored.c
	FROM 
		(SELECT COUNT(*) c FROM unlabeled WHERE processed = FALSE) AS todo, 
		(SELECT COUNT(*) c FROM labeled) as done,
		(SELECT COUNT(*) c FROM unlabeled WHERE ignored = TRUE) as ignored`

	const categories_query string = "SELECT name FROM categories"

	var context TemplateContext = TemplateContext{Id: id}

	db.QueryRow(stats_query).Scan(&context.UnlabeledCount, &context.LabeledCount, &context.IgnoredCount, &context.TotalCount)

	c, _ := db.Query(categories_query)
	for c.Next() {
		var name string
		c.Scan(&name)
		context.Categories = append(context.Categories, name)
	}

	if err := db.QueryRow(mail_query, id).Scan(
		&context.AddressFrom,
		&context.AddressTo,
		&context.Date,
		&context.Subject,
		&context.Text,
		&context.Html,
	); err != nil {
		fmt.Println(err)
		return context, fmt.Errorf("Error when querying the row")
	} else {
		context.UnescapedHtml = template.HTML(context.Html)
		return context, nil
	}
}

func ignoreMail(db *sql.DB, id int) {
	const query string = "UPDATE unlabeled SET ignored = TRUE WHERE id = ?"
	db.Exec(query, id)
}

func deleteMail(db *sql.DB, id int) {
	const query string = "DELETE FROM unlabeled WHERE id = ?"
	db.Exec(query, id)
}

func addNewCategory(db *sql.DB, name string) {
	const query string = "INSERT INTO categories(name) VALUES (?)"
	db.Exec(query, name)
}

func categorizeEmail(db *sql.DB, id int, text string, category string) error {
	context, err := getTemplateContext(db, id)
	if err != nil {
		return err
	}

	const insert_query string = `INSERT INTO labeled(
		unlabeled_id,
		address_from,
		address_to,
		date,
		subject,
		text,
		category
    ) VALUES (?, ?, ?, ?, ?, ?, ?)`

	const update_query string = "UPDATE unlabeled SET processed = TRUE WHERE id = ?"

	tx, _ := db.Begin()
	tx.Exec(insert_query, id, context.AddressFrom, context.AddressTo, context.Date, context.Subject, text, category)
	tx.Exec(update_query, id)
	return tx.Commit()
}

func renderMail(w http.ResponseWriter, req *http.Request, r *render.Render, db *sql.DB, id int) {
	context, err := getTemplateContext(db, id)
	if err == nil {
		r.HTML(w, http.StatusOK, "index", context)
	} else {
		http.NotFound(w, req)
	}
}

func renderRandomMail(w http.ResponseWriter, req *http.Request, db *sql.DB) {
	id, err := getRandomUnlabeledId(db)
	if err != nil {
		http.NotFound(w, req)
	} else {
		http.Redirect(w, req, fmt.Sprintf("/mail/%d", id), 307)
	}
}

func main() {
	c := chi.NewRouter()
	r := render.New()
	db, _ := sql.Open("sqlite3", "../mails.sqlite")

	c.Use(middleware.Logger)
	c.Get("/", func(w http.ResponseWriter, req *http.Request) {
		renderRandomMail(w, req, db)
	})
	c.Get("/mail/{id}", func(w http.ResponseWriter, req *http.Request) {
		id, _ := strconv.Atoi(chi.URLParam(req, "id"))
		renderMail(w, req, r, db, id)
	})
	c.Post("/mail/{id}", func(w http.ResponseWriter, req *http.Request) {
		id, _ := strconv.Atoi(chi.URLParam(req, "id"))
		renderMail(w, req, r, db, id)
	})
	c.Get("/mail/{id}/ignore", func(w http.ResponseWriter, req *http.Request) {
		id, _ := strconv.Atoi(chi.URLParam(req, "id"))
		ignoreMail(db, id)
		renderRandomMail(w, req, db)
	})
	c.Get("/mail/{id}/delete", func(w http.ResponseWriter, req *http.Request) {
		id, _ := strconv.Atoi(chi.URLParam(req, "id"))
		deleteMail(db, id)
		renderRandomMail(w, req, db)
	})
	c.Post("/mail/{id}/newCategory", func(w http.ResponseWriter, req *http.Request) {
		id, _ := strconv.Atoi(chi.URLParam(req, "id"))
		req.ParseForm()
		name := req.Form.Get("name")
		addNewCategory(db, name)
		renderMail(w, req, r, db, id)
	})
	c.Post("/mail/{id}/categorize", func(w http.ResponseWriter, req *http.Request) {
		id, _ := strconv.Atoi(chi.URLParam(req, "id"))
		req.ParseForm()
		text := req.Form.Get("text")
		category := req.Form.Get("category")
		err := categorizeEmail(db, id, text, category)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(err.Error()))
		} else {
			renderRandomMail(w, req, db)
		}
	})
	c.Get("/mail/{id}/iframe", func(w http.ResponseWriter, req *http.Request) {
		id, _ := strconv.Atoi(chi.URLParam(req, "id"))
		context, _ := getTemplateContext(db, id)
		w.Header().Add("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(context.Html))
	})
	fmt.Println("Serving the app on :3000...")
	http.ListenAndServe("localhost:3000", c)
}
