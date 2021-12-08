package main

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/log/log15adapter"
	"github.com/jackc/pgx/v4/pgxpool"
	log "gopkg.in/inconshreveable/log15.v2"
)

/* global variable declaration */
var User, Pass, Host, Port, DBName, DBParams string

func main() {
	LoadEnvVariables()

	Port := os.Getenv("PORT")
	if "" == Port {
		Port = "8080"
	}

	SetupDatabase()
	db := GetDatabaseInstance()

	h := NewBaseHandler(db)
	handlerFunc := http.HandlerFunc(h.RenderSite)
	http.Handle("/", handlerFunc)
	err := http.ListenAndServe(fmt.Sprintf(":%s", Port), nil)
	if err != nil {
		log.Crit("Unable to start web server", "error", err)
		os.Exit(1)
	}
}

func LoadEnvVariables() {
	User = os.Getenv("DB_USER")
	if "" == User {
		User = "postgres"
	}
	Pass = os.Getenv("DB_PASS")
	if "" == Pass {
		Pass = "postgres"
	}
	Host = os.Getenv("DB_HOST")
	if "" == Host {
		Host = "localhost"
	}
	Port = os.Getenv("DB_PORT")
	if "" == Port {
		Port = "5432"
	}
	DBName = os.Getenv("DB_NAME")
	if "" == DBName {
		DBName = "moon"
	}
	DBParams = os.Getenv("DB_PARAMS")
	if "" == DBParams {
		DBParams = "sslmode=disable"
	}
}

type iptype struct {
	IPtype string
	Value  string
}

// BaseHandler will hold everything that controller needs
type BaseHandler struct {
	db *pgxpool.Pool
}

// NewBaseHandler returns a new BaseHandler
func NewBaseHandler(db *pgxpool.Pool) *BaseHandler {
	return &BaseHandler{
		db: db,
	}
}

func (h *BaseHandler) RenderSite(w http.ResponseWriter, r *http.Request) {
	if err := h.db.Ping(context.Background()); err != nil {
		log.Crit("DB Error", "error", err)
		os.Exit(1)
	}

	_, err := h.db.Exec(context.Background(), "UPDATE visits SET counter = counter + 1 WHERE id = 1")
	if err != nil {
		log.Crit("Unable to update the counter", "error", err)
		w.WriteHeader(400)
		w.Write([]byte("Unable to update the counter"))
	}

	var result int
	if err := h.db.QueryRow(context.Background(), "SELECT counter FROM visits WHERE id=1").Scan(&result); err != nil {
		log.Crit("could not read the counter", "error", err)
		w.WriteHeader(400)
		w.Write([]byte("Unable to read the counter value"))
	}

	ip, err := GetIP(r)
	if err != nil {
		w.WriteHeader(400)
		w.Write([]byte("No valid ip"))
	}
	w.WriteHeader(200)
	body := "<!DOCTYPE html><html><head><title>Thanks for your visit!</title></head><body><b>IP addresses:</b>"
	body += "<p>" + ip + "</p>"
	body += "<p><b>Number of visits so far:</b> " + strconv.Itoa(result) + "</p>"
	body += "</body></html>"
	w.Write([]byte(body))
}

func GetIP(r *http.Request) (string, error) {
	var buffer bytes.Buffer
	ips := []*iptype{}
	//Get IP from the X-REAL-IP header
	ip := r.Header.Get("X-REAL-IP")
	netIP := net.ParseIP(ip)
	if netIP != nil {
		ips = append(ips, &iptype{IPtype: "real ip", Value: netIP.String()})
	}

	//Get IP from X-FORWARDED-FOR header
	ipfs := r.Header.Get("X-FORWARDED-FOR")
	splitIps := strings.Split(ipfs, ",")
	for _, ip := range splitIps {
		netIP := net.ParseIP(ip)
		if netIP != nil {
			ips = append(ips, &iptype{IPtype: "forwarded for", Value: netIP.String()})
		}
	}

	//Get IP from RemoteAddr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		netIP = net.ParseIP(ip)
		if netIP != nil {
			ips = append(ips, &iptype{IPtype: "ip from remote addr", Value: netIP.String()})
		}
	}

	for _, ip := range ips {
		buffer.WriteString(fmt.Sprintf("<b>Type:</b> %s, <b>value:</b> %s<br>", ip.IPtype, ip.Value))
	}

	if len(ips) > 0 {
		return buffer.String(), nil
	}

	return "", fmt.Errorf("No valid ip found")
}

func SetupDatabase() {
	logger := log15adapter.NewLogger(log.New("module", "pgx"))

	poolConfig, err := pgxpool.ParseConfig(fmt.Sprintf("postgresql://%s:%s@%s:%s/?%s", User, Pass, Host, Port, DBParams))
	if err != nil {
		log.Crit("Wrong database config", "error", err)
		os.Exit(1)
	}

	poolConfig.ConnConfig.Logger = logger

	db, err := pgxpool.ConnectConfig(context.Background(), poolConfig)
	if err != nil {
		log.Crit("Unable to create connection pool", "error", err)
		os.Exit(1)
	}

	if err := db.Ping(context.Background()); err != nil {
		log.Crit("unable to reach database", "error", err)
		os.Exit(1)
	}

	var result string
	err = db.QueryRow(context.Background(), "SELECT datname FROM pg_catalog.pg_database WHERE datname=$1", DBName).Scan(&result)
	switch err {
	case nil:
		return
	case pgx.ErrNoRows:
		if _, err := db.Exec(context.Background(), fmt.Sprintf("CREATE DATABASE %s", DBName)); err != nil {
			log.Crit("could not create database", "error", err)
			os.Exit(1)
		}
		SetupTable()
	default:
		log.Crit("Unable to create database", "error", err)
		os.Exit(1)
	}
}

func SetupTable() {
	logger := log15adapter.NewLogger(log.New("module", "pgx"))

	poolConfig, err := pgxpool.ParseConfig(fmt.Sprintf("postgresql://%s:%s@%s:%s/%s?%s", User, Pass, Host, Port, DBName, DBParams))
	if err != nil {
		log.Crit("Wrong database config", "error", err)
		os.Exit(1)
	}

	poolConfig.ConnConfig.Logger = logger

	db, err := pgxpool.ConnectConfig(context.Background(), poolConfig)
	if err != nil {
		log.Crit("Unable to create connection pool", "error", err)
		os.Exit(1)
	}

	if _, err := db.Exec(context.Background(), fmt.Sprintf("CREATE TABLE visits (id integer PRIMARY KEY, counter integer)")); err != nil {
		log.Crit("could not create table", "error", err)
	}
	if _, err := db.Exec(context.Background(), fmt.Sprintf("INSERT INTO visits (id, counter) VALUES (%d, %d)", 1, 0)); err != nil {
		log.Crit("could not initiate counter", "error", err)
	}
}

func GetDatabaseInstance() *pgxpool.Pool {
	logger := log15adapter.NewLogger(log.New("module", "pgx"))

	poolConfig, err := pgxpool.ParseConfig(fmt.Sprintf("postgresql://%s:%s@%s:%s/%s?%s", User, Pass, Host, Port, DBName, DBParams))
	if err != nil {
		log.Crit("Wrong database config", "error", err)
		os.Exit(1)
	}

	poolConfig.ConnConfig.Logger = logger

	db, err := pgxpool.ConnectConfig(context.Background(), poolConfig)
	if err != nil {
		log.Crit("Unable to create connection pool", "error", err)
		os.Exit(1)
	}
	return db
}
