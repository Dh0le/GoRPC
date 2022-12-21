package xclient

import (
	"errors"
	"math"
	"math/rand"
	"sync"
	"time"
)

type SelectMode int 

const(
	RandomSelect SelectMode=iota //randome select
	RoundRobinSelect // round robin select
)

type Discovery interface{
	Refresh() error
	Update(servers []string)
	Get(mode SelectMode)(string,error)
	GetAll()([]string,error)
}

type MultiServerDiscovery struct{
	r *rand.Rand // generate random number
	mu sync.RWMutex // protect 
	servers []string
	index int // record the select position for round robin algorithm
}

// create a multiserver discovery instance
func newMultiServerDiscovery(servers []string) *MultiServerDiscovery{
	d := &MultiServerDiscovery{
		servers: servers,
		r: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
	// initilize the index to a random value to avoid always start from 0
	d.index = d.r.Intn(math.MaxInt32-1)
	return d
}

var _Discovery = (*MultiServerDiscovery)(nil)

func (d *MultiServerDiscovery)Refresh()error{
	return nil
}

// update server dynamiclly 
func (d *MultiServerDiscovery)Update(servers []string)error{
	d.mu.Lock()
	defer d.mu.Unlock()

	d.servers = servers
	return nil
}

// get the server based on current load balancing mode
func (d *MultiServerDiscovery)Get(mode SelectMode)(string,error){
	d.mu.Lock()
	defer d.mu.Unlock()
	n := len(d.servers)

	if n == 0{
		return "", errors.New("rpc discovery: no available servers")
	}

	switch mode{
	case RandomSelect:
		return d.servers[d.r.Intn(n)],nil
	case RoundRobinSelect:
		s := d.servers[d.index%n]
		d.index = (d.index+1)%n
		return s,nil
	default:
		return "", errors.New("rpc discovery: not supported select mode")
	}
}

// return a copy of all server in discovery
func (d *MultiServerDiscovery)GetAll()([]string,error){
	d.mu.Lock()
	defer d.mu.Unlock()
	servers := make([]string, len(d.servers), len(d.servers))
	copy(servers,d.servers)
	return servers,nil
}



