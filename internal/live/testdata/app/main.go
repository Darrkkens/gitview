// liveapp is the HTTP server the runtime inspector tests monitor.
package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"strconv"
	"time"

	"example.com/liveapp/fakedb"
	"example.com/liveapp/store"
	"github.com/athos/gitview/collector"
)

var (
	addr     = flag.String("addr", "127.0.0.1:0", "listen address")
	useColl  = flag.Bool("collector", false, "enable GitView's collector")
	usePprof = flag.Bool("pprof", false, "serve net/http/pprof on a separate loopback port")
	extra    = flag.Duration("extra", 0, "extra work per request (to emulate a regression)")
	dbDelay  = flag.Duration("db", 3*time.Millisecond, "time each SQL statement takes")
)

type api struct{ st *store.Store }

// burn spins for d.
func burn(d time.Duration) int {
	end := time.Now().Add(d)
	x := 0
	for time.Now().Before(end) {
		for i := 0; i < 1000; i++ {
			x += i
		}
	}
	return x
}

func (a *api) getClient(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	burn(5*time.Millisecond + *extra)
	name, err := a.st.ClientName(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"id": id, "name": name})
}

func (a *api) health(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) }

func main() {
	flag.Parse()
	fakedb.Delay = *dbDelay
	db, err := sql.Open("fakedb", "")
	if err != nil {
		panic(err)
	}
	a := &api{st: &store.Store{DB: db}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /clients/{id}", a.getClient)
	mux.HandleFunc("GET /health", a.health)
	var h http.Handler = mux
	if *useColl {
		h = collector.Middleware(mux)
		caddr, err := collector.Default().Start("")
		if err != nil {
			panic(err)
		}
		fmt.Println("COLLECTOR", caddr)
		defer collector.Default().Close()
	}
	if *usePprof {
		pm := http.NewServeMux()
		pm.HandleFunc("/debug/pprof/", pprof.Index)
		pm.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		pm.HandleFunc("/debug/pprof/profile", pprof.Profile)
		pm.HandleFunc("/debug/pprof/trace", pprof.Trace)
		pl, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			panic(err)
		}
		go http.Serve(pl, pm)
		fmt.Println("PPROF", pl.Addr())
	}
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		panic(err)
	}
	fmt.Println("LISTEN", ln.Addr())
	os.Stdout.Sync()
	http.Serve(ln, h)
}
