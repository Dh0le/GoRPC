package xclient

import (
	"log"
	"net/http"
	"strings"
	"time"
)

type GoRegistryDiscovery struct{
	// use the multiServer Discovery we implement earlier
	*MultiServerDiscovery
	registry string // registry address
	timeout time.Duration // time duration we need to update our server list
	lastUpdate time.Time // last update time
}

const defaultUpdateTimeout = time.Second*10

// new registry discovery
func NewGoRegistryDiscovery(registryAddr string, timeout time.Duration)*GoRegistryDiscovery{
	if timeout == 0{
		timeout = defaultUpdateTimeout
	}

	d := &GoRegistryDiscovery{
		MultiServerDiscovery: newMultiServerDiscovery(make([]string, 0)),
		registry: registryAddr,
		timeout: timeout,
	}
	return d
}

// update current discovery with list of new server
func (d *GoRegistryDiscovery)Update(servers []string)error{
	d.mu.Lock()
	defer d.mu.Unlock()
	d.servers = servers
	d.lastUpdate = time.Now()
	return nil
}

func (d *GoRegistryDiscovery)Refresh()error{
	d.mu.Lock()
	defer d.mu.Unlock()
	// check if update time is reached
	if d.lastUpdate.Add(d.timeout).After(time.Now()){
		return nil
	}

	log.Println("rpc discovery: refresh server from registry",d.registry)
	resp, err := http.Get(d.registry)
	if err != nil{
		log.Println("rpc discovery: refresh error",err.Error())
		return err
	}

	servers := strings.Split(resp.Header.Get("GoRPC-Servers"),",")
	d.servers = make([]string, 0,len(servers))
	for _,server := range servers{
		if strings.TrimSpace(server) != ""{
			d.servers = append(d.servers, strings.TrimSpace(server))
		}
	}
	d.lastUpdate = time.Now()
	return nil
}

func (d *GoRegistryDiscovery)Get(mode SelectMode)(string,error){
	if err := d.Refresh();err != nil{
		return "",err
	}
	return d.MultiServerDiscovery.Get(mode)
}

func (d *GoRegistryDiscovery)GetAll()([]string, error){
	if err := d.Refresh();err != nil{
		return nil,err
	}
	return d.MultiServerDiscovery.GetAll()
}



