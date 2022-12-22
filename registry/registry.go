package registry

import (
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// registry struct
type GoRegistry struct{
	timeout time.Duration // timeout for registared server
	mu sync.Mutex // lock to protect concurrent operation
	servers map[string] *ServerItem // map to store registered server
}


// server item in registry
type ServerItem struct{
	Addr string // address
	start time.Time //registar time
}

const (
	defaultPath = "/_gorpc_/registry"
	defaultTimeout = time.Minute * 5
)

// constructor for registry
func New(timeout time.Duration)*GoRegistry{
	return &GoRegistry{
		timeout: timeout,
		servers: make(map[string]*ServerItem),
	}
}

// 
var DefaultGoRegistry = New(defaultTimeout)

// register a server to the registry center
func (r *GoRegistry)putServer(addr string){
	r.mu.Lock()
	defer r.mu.Unlock()

	s := r.servers[addr]

	if s == nil{
		r.servers[addr] = &ServerItem{Addr: addr,start: time.Now()}
	}else{
		// if server already exist, update start time
		s.start = time.Now()
	}
}

// return all alive server
func (r *GoRegistry)aliveServers()[]string{
	r.mu.Lock()
	defer r.mu.Unlock()
	var alive []string
	for addr,s := range r.servers{
		
		if r.timeout == 0 || s.start.Add(r.timeout).After(time.Now()){
			// if current server is within timeout range
			alive = append(alive, addr)
		}else{
			// if current server is already out of timeout
			delete(r.servers,addr)
		}
	}
	sort.Strings(alive)
	return alive
}
// serve http at default registry path
func (r *GoRegistry)ServeHTTP(w http.ResponseWriter,req *http.Request){
	switch req.Method{
	case "GET":
		// get request will return all alive servers address
		w.Header().Set("GoRPC-Servers",strings.Join(r.aliveServers(),","))
	case "POST":
		// post will register a serer
		addr := req.Header.Get("GoRPC-Servers")
		if addr == ""{
			w.WriteHeader(http.StatusInternalServerError)
			return 
		}
		r.putServer(addr)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (r* GoRegistry)HandleHTTP(registryPath string){
	http.Handle(registryPath,r)
	log.Println("go rpc registry path:",registryPath)
}

func HandleHTTP(){
	DefaultGoRegistry.HandleHTTP(defaultPath)
}

// heartbeat function
// registry: registry address
// addr: local address
func Heartbeat(registry, addr string, duration time.Duration){
	// makesure enough time for next heartbeat
	if duration == 0{
		duration = defaultTimeout - time.Duration(1)*time.Minute
	}

	var err error
	// send heartbeat to registry first
	err = sendHeartbeat(registry,addr)
	go func(){
		//start a timer and send heartbeat once a while
		t := time.NewTicker(duration)
		for err == nil{
			<-t.C
			err = sendHeartbeat(registry,addr)
		}
	}()
}

// send heartbeat to registry
// registry: registry address
// addr: local address
func sendHeartbeat(registry, addr string)error{
	log.Println(addr,"send heartbeat to registry")
	httpClient := &http.Client{}
	req, _ := http.NewRequest("POST",registry,nil)
	req.Header.Set("GoRPC-Servers",addr)
	if _,err := httpClient.Do(req);err != nil{
		log.Println("rpc server:heartbeat error",err.Error())
		return err
	}
	return nil
}

