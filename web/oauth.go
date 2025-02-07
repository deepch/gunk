package web

import (
	"crypto/hmac"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"log"
	"net/http"

	"golang.org/x/oauth2"
)

const (
	stateCookieExpires = 15 * 60
	loginCookieExpires = 30 * 24 * 60 * 60
)

var discordEndpoint = oauth2.Endpoint{
	AuthURL:   "https://discordapp.com/api/oauth2/authorize",
	TokenURL:  "https://discordapp.com/api/oauth2/token",
	AuthStyle: oauth2.AuthStyleInHeader,
}

func (s *Server) SetOauth(clientID, clientSecret string) {
	s.oauth = oauth2.Config{
		RedirectURL:  s.BaseURL + "/oauth2/cb",
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     discordEndpoint,
		Scopes:       []string{"identify", "guilds"},
	}
}

func (s *Server) viewUser(rw http.ResponseWriter, req *http.Request) {
	var info discordUser
	if err := s.unseal(req, loginCookie, &info); err != nil {
		info = discordUser{}
	}
	if info.Avatar != "" {
		info.Avatar = "/avatars/" + info.ID + "/" + info.Avatar + ".png"
	}
	writeJSON(rw, info)
}

func (s *Server) viewOauthLogin(rw http.ResponseWriter, req *http.Request) {
	if s.oauth.ClientID == "" {
		http.Error(rw, "oauth not configured", 400)
		return
	}
	sb := make([]byte, 9)
	if _, err := io.ReadFull(rand.Reader, sb); err != nil {
		panic(err)
	}
	state := base64.RawURLEncoding.EncodeToString(sb)
	s.setCookie(rw, stateCookie, state, stateCookieExpires)
	http.Redirect(rw, req, s.oauth.AuthCodeURL(state), http.StatusFound)
}

func (s *Server) viewOauthCB(rw http.ResponseWriter, req *http.Request) {
	if s.oauth.ClientID == "" {
		http.Error(rw, "oauth not configured", 400)
		return
	}
	token, err := s.tokenExchange(rw, req)
	if err != nil {
		log.Printf("[oauth] error: %s: %s", req.RemoteAddr, err)
		http.Error(rw, "oauth failure", 400)
		return
	}
	user, err := s.lookupUser(req.Context(), token)
	if err != nil {
		log.Printf("[oauth] error: %s: %s", req.RemoteAddr, err)
		http.Error(rw, "error getting user info from discord", 400)
		return
	}
	if err := s.setCookie(rw, loginCookie, user, loginCookieExpires); err != nil {
		log.Printf("[oauth] error: persisting login: %s", err)
		http.Error(rw, "error setting login cookie", 500)
		return
	}
	http.Redirect(rw, req, "/", http.StatusFound)
}

func (s *Server) tokenExchange(rw http.ResponseWriter, req *http.Request) (*oauth2.Token, error) {
	code := req.FormValue("code")
	if code == "" {
		return nil, errors.New("missing code")
	}
	state := req.FormValue("state")
	var state2 string
	err := s.unseal(req, stateCookie, &state2)
	s.setCookie(rw, stateCookie, nil, -1)
	if err != nil {
		return nil, err
	} else if !hmac.Equal([]byte(state2), []byte(state)) {
		return nil, errors.New("state mismatch")
	}
	return s.oauth.Exchange(req.Context(), code)
}

func (s *Server) viewOauthLogout(rw http.ResponseWriter, req *http.Request) {
	s.setCookie(rw, loginCookie, nil, -1)
	rw.Header().Set("Content-Type", "application/json")
	rw.Write([]byte("{}"))
}
