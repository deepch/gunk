// Copyright © Michael Tharp <gxti@partiallystapled.com>
//
// This file is distributed under the terms of the MIT License.
// See the LICENSE file at the top of this tree or http://opensource.org/licenses/MIT

package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"path"
	"time"

	"github.com/gorilla/mux"
	"github.com/jackc/pgx"
)

var db *pgx.ConnPool

func connectDB() error {
	conf, err := pgx.ParseEnvLibpq()
	if err != nil {
		return err
	}
	db, err = pgx.NewConnPool(pgx.ConnPoolConfig{
		ConnConfig:     conf,
		MaxConnections: 10,
	})
	return err
}

type channelDef struct {
	Name     string `json:"name"`
	Key      string `json:"key"`
	Announce bool   `json:"announce"`

	RTMPDir  string `json:"rtmp_dir"`
	RTMPBase string `json:"rtmp_base"`
}

func (d *channelDef) setURL(base string) {
	v := url.Values{"key": []string{d.Key}}
	d.RTMPDir = base
	d.RTMPBase = url.PathEscape(d.Name) + "?" + v.Encode()
}

func getChannelDefs(userID string) (defs []*channelDef, err error) {
	rows, err := db.Query("SELECT name, key, announce FROM channel_defs WHERE user_id = $1", userID)
	if err != nil {
		return
	}
	defer rows.Close()
	defs = []*channelDef{}
	for rows.Next() {
		def := new(channelDef)
		if err = rows.Scan(&def.Name, &def.Key, &def.Announce); err != nil {
			return
		}
		defs = append(defs, def)
	}
	err = rows.Err()
	return
}

func createChannel(userID, name string) (def *channelDef, err error) {
	b := make([]byte, 24)
	if _, err = io.ReadFull(rand.Reader, b); err != nil {
		return
	}
	key := hex.EncodeToString(b)
	_, err = db.Exec("INSERT INTO channel_defs (user_id, name, key, announce) VALUES ($1, $2, $3, true)", userID, name, key)
	if err != nil {
		return
	}
	return &channelDef{Name: name, Key: key, Announce: true}, nil
}

func updateChannel(userID, name string, announce bool) error {
	tag, err := db.Exec("UPDATE channel_defs SET announce = $1 WHERE user_id = $2 AND name = $3", announce, userID, name)
	if err != nil {
		return err
	} else if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func deleteChannel(userID, name string) error {
	_, err := db.Exec("DELETE FROM channel_defs WHERE user_id = $1 AND name = $2", userID, name)
	return err
}

var ErrUserNotFound = errors.New("user not found or wrong key")

type channelAuth struct {
	UserID   string
	Name     string
	Announce bool
}

func findChannel(column, value string) (auth channelAuth, key string, err error) {
	row := db.QueryRow("SELECT user_id, channel_defs.name, channel_defs.key, COALESCE(channel_defs.announce AND users.announce, false) FROM channel_defs LEFT JOIN users USING (user_id) WHERE "+column+" = $1", value)
	err = row.Scan(&auth.UserID, &auth.Name, &key, &auth.Announce)
	return
}

func verifyRTMP(u *url.URL) (auth channelAuth, err error) {
	var expectKey string
	auth, expectKey, err = findChannel("name", path.Base(u.Path))
	if err != nil {
		if err == pgx.ErrNoRows {
			err = ErrUserNotFound
		}
		return
	}
	key := u.Query().Get("key")
	if !hmac.Equal([]byte(key), []byte(expectKey)) {
		err = ErrUserNotFound
		return
	}
	return
}

func verifyFTL(channelID string, nonce, hmacProvided []byte) (interface{}, error) {
	auth, key, err := findChannel("ftl_id", channelID)
	if err != nil {
		if err == pgx.ErrNoRows {
			err = ErrUserNotFound
		}
		return nil, err
	}
	hm := hmac.New(sha512.New, []byte(key))
	hm.Write(nonce)
	expected := hm.Sum(nil)
	if !hmac.Equal(expected, hmacProvided) {
		log.Printf("error: hmac digest mismatch for FTL channel %s", auth.Name)
		return nil, ErrUserNotFound
	}
	return auth, nil
}

func getThumb(channelName string) (d []byte, err error) {
	row := db.QueryRow("SELECT thumb FROM thumbs WHERE name = $1", channelName)
	err = row.Scan(&d)
	return
}

func putThumb(channelName string, d []byte) error {
	_, err := db.Exec("INSERT INTO thumbs (name, thumb) VALUES ($1, $2) ON CONFLICT (name) DO UPDATE SET thumb = EXCLUDED.thumb, updated = now()", channelName, d)
	return err
}

func (s *gunkServer) checkAuth(rw http.ResponseWriter, req *http.Request) string {
	var info loginInfo
	err := s.unseal(req, s.loginCookie, &info)
	if err == nil {
		return info.ID
	}
	log.Printf("error: authentication failed for %s to %s", req.RemoteAddr, req.URL)
	http.Error(rw, "not authorized", 401)
	return ""
}

func (s *gunkServer) viewDefs(rw http.ResponseWriter, req *http.Request) {
	userID := s.checkAuth(rw, req)
	if userID == "" {
		return
	}
	defs, err := getChannelDefs(userID)
	if err != nil {
		log.Println("error:", err)
		http.Error(rw, "", 500)
	}
	for _, def := range defs {
		def.setURL(s.rtmpBase)
	}
	blob, _ := json.Marshal(defs)
	rw.Header().Set("Content-Type", "application/json")
	rw.Write(blob)
}

func parseRequest(rw http.ResponseWriter, req *http.Request, d interface{}) bool {
	blob, err := ioutil.ReadAll(req.Body)
	if err != nil {
		log.Printf("error: reading %s request: %s", req.RemoteAddr, err)
		http.Error(rw, "", 500)
		return false
	}
	if err := json.Unmarshal(blob, d); err != nil {
		log.Printf("error: reading %s request: %s", req.RemoteAddr, err)
		http.Error(rw, "invalid JSON in request", 400)
		return false
	}
	return true
}

type defRequest struct {
	Name string `json:"name"`
}

func (s *gunkServer) viewDefsCreate(rw http.ResponseWriter, req *http.Request) {
	userID := s.checkAuth(rw, req)
	if userID == "" {
		return
	}
	var dr defRequest
	if !parseRequest(rw, req, &dr) {
		return
	}
	def, err := createChannel(userID, dr.Name)
	if err != nil {
		if pge, ok := err.(pgx.PgError); ok && pge.Code == "23505" {
			http.Error(rw, "channel name already in use", http.StatusConflict)
			return
		}
		log.Printf("error: creating channel %q for %s: %s", dr.Name, req.RemoteAddr, err)
		http.Error(rw, "", 500)
		return
	}
	def.setURL(s.rtmpBase)
	blob, _ := json.Marshal(def)
	rw.Header().Set("Content-Type", "application/json")
	rw.Write(blob)
}

type defUpdate struct {
	Announce bool `json:"announce"`
}

func (s *gunkServer) viewDefsUpdate(rw http.ResponseWriter, req *http.Request) {
	userID := s.checkAuth(rw, req)
	if userID == "" {
		return
	}
	var du defUpdate
	if !parseRequest(rw, req, &du) {
		return
	}
	name := mux.Vars(req)["name"]
	if err := updateChannel(userID, name, du.Announce); err != nil {
		log.Printf("error: updating channel %q for %s: %s", name, req.RemoteAddr, err)
		http.Error(rw, "", 500)
		return
	}
	rw.Header().Set("Content-Type", "application/json")
	rw.Write([]byte("{}"))
	return
}

func (s *gunkServer) viewDefsDelete(rw http.ResponseWriter, req *http.Request) {
	userID := s.checkAuth(rw, req)
	if userID == "" {
		return
	}
	name := mux.Vars(req)["name"]
	if err := deleteChannel(userID, name); err != nil {
		log.Printf("error: deleting channel %q for %s: %s", name, req.RemoteAddr, err)
		return
	}
	rw.Header().Set("Content-Type", "application/json")
	rw.Write([]byte("{}"))
}

type channelInfo struct {
	Name    string `json:"name"`
	Live    bool   `json:"live"`
	Last    int64  `json:"last"`
	Thumb   string `json:"thumb"`
	LiveURL string `json:"live_url"`
}

func listChannels() (ret []*channelInfo, err error) {
	rows, err := db.Query("SELECT name, updated FROM thumbs ORDER BY greatest(now() - updated, '1 minute'::interval) ASC, 1 ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		info := new(channelInfo)
		var last time.Time
		if err := rows.Scan(&info.Name, &last); err != nil {
			return nil, err
		}
		info.Last = last.UnixNano() / 1000000
		ret = append(ret, info)
	}
	err = rows.Err()
	return
}
