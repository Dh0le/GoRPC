package xclient

import (
	"context"
	"fmt"
	. "gorpc"
	"io"
	"reflect"
	"sync"

	"golang.org/x/net/context"
)

type XClient struct{
	d Discovery
	mode SelectMode
	opt *Option
	mu sync.Mutex
	clients map[string]*Client
}


var _ io.Closer = (*XClient)(nil)

func NewXClient(d Discovery, mode SelectMode, opt *Option)*XClient{
	return &XClient{
		d: d,
		mode: mode,
		opt: opt,
		clients: make(map[string]*Client),
	}
}

// close all the client
func(xc *XClient)Close()error{
	xc.mu.Lock()
	defer xc.mu.Unlock()

	for key, client := range xc.clients{
		err := client.Close()
		if err != nil{
			fmt.Printf("rpc xclient: %s",err.Error())
		}
		delete(xc.clients,key)
	}
	
	return nil
}

// we check if we have cached client in pool, if so check status
// if not delete
func (xc *XClient)dial(rpcAddr string)(*Client,error){
	xc.mu.Lock()
	defer xc.mu.Unlock()

	// check cached client in pool
	client,ok := xc.clients[rpcAddr]
	if ok && !client.IsAvailable(){
		_ = client.Close()
		delete(xc.clients,rpcAddr)
		client = nil
	}
	// no cached client
	if client == nil{
		var err error
		// create new client
		client, err = XDial(rpcAddr,xc.opt)
		if err != nil{
			return nil,err
		}
		// cached current client into pool
		xc.clients[rpcAddr] = client
	}
	return client, nil
}

func (xc *XClient)call(rpcAddr string, ctx context.Context, serviceMethod string, args, reply interface{})error{
	// get a client and call service
	client,err := xc.dial(rpcAddr)
	if err != nil{
		return err
	}
	return client.Call(ctx,serviceMethod,args,reply)
}

func (xc *XClient)Call(ctx context.Context, serviceMethod string, args,reply interface{})error{
	// based on the discovery center and mode, get next availaible address
	rpcAddr, err := xc.d.Get(xc.mode)
	if err != nil{
		return err
	}
	return xc.call(rpcAddr,ctx,serviceMethod,args,reply)
}

// boardcast function will boardcast the rpc to all availiable serivece instance
func (xc *XClient)Broadcast(ctx context.Context ,serviceMethod string, args,reply interface{})error{
	// get all server instances
	servers, err := xc.d.GetAll()
	if err != nil{
		return err
	}
	var wg sync.WaitGroup // waitgroup for all the calls
	var mu sync.Mutex // mutex lock for assign reply value
	var e error // return error

	// if reply is nil, not thing to do
	replyDone := reply == nil
	// use cancel we can fail fast if we encounter some problem
	ctx, cancel := context.WithCancel(ctx)
	// boardcast to all server instance
	for _,rpcAddr := range servers{
		wg.Add(1)
		go func(rpcAddr string){
			defer wg.Done()
			// clone a new reply address
			var cloneReply interface{}
			if reply != nil{
				cloneReply = reflect.New(reflect.ValueOf(reply).Elem().Type()).Interface()
			}
			// start the call
			err := xc.call(rpcAddr,ctx,serviceMethod,args,reply)
			// lock to make sure reply is been corretly assigned
			mu.Lock()
			// if error happens
			if err != nil && e == nil{
				e = err
				cancel() // if a call failed, cancel all unfinished call
			}
			// this is the first reply
			if err == nil && !replyDone{
				reflect.ValueOf(reply).Elem().Set(reflect.ValueOf(cloneReply).Elem())
				replyDone = true
			}
			mu.Unlock()
		}(rpcAddr)
	}
	wg.Wait()
	return e
}