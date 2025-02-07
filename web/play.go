package web

import (
	"log"
	"net/http"

	"eaglesong.dev/gunk/ingest"
	"github.com/gorilla/mux"
)

func (s *Server) viewPlayHLS(rw http.ResponseWriter, req *http.Request) {
	chname := mux.Vars(req)["channel"]
	err := s.Channels.ServeHLS(rw, req, chname)
	if err == ingest.ErrNoChannel {
		http.NotFound(rw, req)
	} else if err != nil {
		log.Println("error:", err)
	}
}

func (s *Server) viewPlayTS(rw http.ResponseWriter, req *http.Request) {
	chname := mux.Vars(req)["channel"]
	err := s.Channels.ServeTS(rw, req, chname)
	if err == ingest.ErrNoChannel {
		http.NotFound(rw, req)
	} else if err != nil {
		log.Println("error:", err)
	}
}

func (s *Server) viewPlaySDP(rw http.ResponseWriter, req *http.Request) {
	chname := mux.Vars(req)["channel"]
	err := s.Channels.ServeSDP(rw, req, chname)
	if err == ingest.ErrNoChannel {
		http.NotFound(rw, req)
	} else if err != nil {
		log.Printf("error: failed to start webrtc session to %s: %s", req.RemoteAddr, err)
		http.Error(rw, "failed to start webrtc session", 500)
	}
}
