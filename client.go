package gorpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"gorpc/codec"
	"io"
	"log"
	"net"
	"sync"
	"time"
)

type Call struct{
	Seq uint64
	ServiceMethod string
	Args interface{}
	Reply interface{}
	Error error
	Done chan *Call
}

func (call *Call)done(){
	call.Done <- call
}


// a RPC client
// asycn supported, concurrent call supported
type Client struct{
	cc codec.Codec
	opt *Option
	// sending process needs to be sequential
	sending sync.Mutex // protect
	header codec.Header
	// operation need to be sequential
	mu sync.Mutex // protect
	seq uint64 // unqiue id for each call
	pending map[uint64]*Call //ongoing calls
	closing bool // client send stop
	shutdown bool // server send stop
}

var _ io.Closer = (*Client)(nil)
// any call while the client is shutting down will trigger this function
var ErrShutdown = errors.New("connection is shut down")

// close current client
func(client *Client)Close()error{
	client.mu.Lock()
	defer client.mu.Unlock()
	// if some other route send close
	if client.closing{
		return ErrShutdown
	}
	client.closing = true
	return client.cc.Close()
}

func (client *Client)IsAvailable()bool{
	client.mu.Lock()
	defer client.mu.Unlock()
	return !client.shutdown && !client.closing
}

// this will register a call in client
func(client *Client)registerCall(call *Call)(uint64,error){
	client.mu.Lock()
	defer client.mu.Unlock()
	// client cannot be shut down or closing
	if client.closing || client.shutdown{
		return 0,ErrShutdown
	}
	// call will have unique id
	call.Seq = client.seq
	// add into pending
	client.pending[call.Seq] = call
	// increment id
	client.seq++
	return call.Seq,nil
}
// remove corresponding calls
func(client *Client)removeCall(seq uint64)*Call{
	client.mu.Lock()
	defer client.mu.Unlock()

	call := client.pending[seq]
	delete(client.pending,seq)
	return call
}

// remove wrong call, shutdown all the calls
func(client *Client)terminateCalls(err error){
	// hold lock for any sending or operation
	client.sending.Lock()
	defer client.sending.Lock()
	client.mu.Lock()
	defer client.mu.Unlock()

	client.shutdown = true
	for _, call := range client.pending{
		call.Error = err
		call.done()
	}
}

// recieve response from server
func(client *Client)receive(){
	var err error
	// update error in the loop
	for err == nil{
		var h codec.Header
		if err = client.cc.ReadHeader(&h);err != nil{
			break
		}
		call := client.removeCall(h.Seq)
		switch{
		case call == nil:
			// request not sent completely or cancled, by still processed the server
			err = client.cc.ReadBody(nil)
		case h.Error != "":
			// serverside return an error back
			call.Error = fmt.Errorf(h.Error)
			err = client.cc.ReadBody(nil)
			call.done()
		default:
			// assume no error, readbody into call.reply
			err = client.cc.ReadBody(call.Reply)
			if err != nil{
				// if reading error occurs place it into call error
				call.Error = errors.New("reading body "+err.Error())
			}
			call.done()
		}
	}
	// if an error occurs, terminate all calls
	client.terminateCalls(err)
}

func(client *Client)send(call *Call){
	// make sure request sent complete
	client.sending.Lock()
	defer client.sending.Unlock()

	seq, err := client.registerCall(call)
	if err != nil{
		call.Error = err
		call.done()
		return
	}
	// place info in header
	client.header.ServiceMethod = call.ServiceMethod
	client.header.Seq = seq
	client.header.Error = ""
	// encode and send request to the server
	if err := client.cc.Write(&client.header,call.Args);err != nil{
		call := client.removeCall(call.Seq)
		// if call is not nil, which usually means its partially failed.
		// client has recieved the reponsee and handled
		if call != nil{
			call.Error = err
			call.done()
		}
	}
}

func(client *Client)Go(serviceMethod string, args, reply interface{}, done chan *Call)*Call{
	if done == nil{
		done = make(chan *Call,10)
	}else if cap(done) == 0{
		log.Panic("rpc client: done channl is unbuffered")
	}

	call := &Call{
		ServiceMethod: serviceMethod,
		Args: args,
		Reply: reply,
		Done: done,
	}
	
	client.send(call)
	return call
}

func(client *Client)Call(ctx context.Context,serviceMethod string, args, reply interface{})error{
	// user can use context withtime out to add timeout during call
	call := client.Go(serviceMethod,args,reply,make(chan *Call,1))
	select{
	case <-ctx.Done():
		client.removeCall(call.Seq)
		return errors.New("rpc client:call failed:"+ctx.Err().Error())
	case call:= <-call.Done:
		return call.Error
	}
}

type clientResult struct{
	client *Client
	err error
}
type newClientFunc func(conn net.Conn, opt *Option) (client *Client, err error)

func dialTimeout(f newClientFunc, network,address string, opts ...*Option)(client *Client,err error){
	// parse option
	opt,err := parseOptions(opts...)
	if err != nil{
		return nil, err
	}
	// dial with timeout
	conn,err := net.DialTimeout(network,address,opt.ConnectTimeout)
	if err != nil{
		return nil,err
	}

	defer func(){
		if err != nil{
			_ = conn.Close()
		}
	}()
	// channel to place connetion result
	ch := make(chan clientResult)
	
	// create client
	go func(){
		client, err := f(conn,opt)
		ch <- clientResult{client:client,err: err}
	}()
	
	// not timeout
	if opt.ConnectTimeout == 0{
		result := <-ch
		return result.client,err
	}
	// check weather timeout reach first or result reach first
	select{
	case <- time.After(opt.ConnectTimeout):
		return nil, fmt.Errorf("rpc client:connect timeout:expect within %s",opt.ConnectTimeout)
	case result := <-ch:
		return result.client,result.err
	}
}


// constructor for client
func NewClient(conn net.Conn, opt *Option)(*Client, error){
	// check if codec function can be found
	f := codec.NewCodecFuncMap[opt.CodecType]
	if f == nil{
		err := fmt.Errorf("invalid codec type %s",opt.CodecType)
		log.Println("rpc client:codec error:",err)
		return nil, err
	}

	// send opt to server to check validity.
	if err := json.NewEncoder(conn).Encode(opt);err != nil{
		log.Println("rpc client:options error:",err)
		_ = conn.Close()
		return nil, err
	}
	// create codec and client
	return newClientCodec(f(conn),opt),nil
}

func newClientCodec(cc codec.Codec,opt *Option)*Client{
	client := &Client{
		seq: 1,
		cc: cc,
		opt: opt,
		pending: make(map[uint64]*Call),
	}
	// start reciveing response
	go client.receive()
	return client
}

// we allow user to enter option or just using default
func parseOptions(opts ...*Option)(*Option,error){
	if len(opts) == 0 || opts[0] == nil{
		return DefaultOption,nil
	}
	if len(opts) != 1{
		return nil, errors.New("number of options is more than one")
	}

	opt := opts[0]
	opt.MagicNumber = DefaultOption.MagicNumber
	if(opt.CodecType == ""){
		opt.CodecType = DefaultOption.CodecType
	}
	return opt,nil
}

// dial function that allows user to pass in address and port, option is optional
func Dial(network, address string, opts...*Option)(client *Client,err error){
	return dialTimeout(NewClient,network,address,opts...)
}







