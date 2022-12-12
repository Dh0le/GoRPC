package gorpc

import (
	"encoding/json"
	"fmt"
	"gorpc/codec"
	"io"
	"log"
	"net"
	"reflect"
	"sync"
)

const MagicNumber = 0x3bef5c

// Options should be contained in the meta data arrived first in the connection
type Option struct{
	MagicNumber int // we use this magic number to mark a go rpc request
	CodecType codec.Type // client may use different type of encoding
}


var DefaultOption = &Option{
	MagicNumber: MagicNumber,
	CodecType: codec.GobType,
}

// RPC server
type Server struct{}

// Constructor
func NewServer() *Server{
	return &Server{}
}

var DefaultServer = NewServer()

// to start a server, just pass in a listener object, it support both tcp and unix
func (server *Server)Accept(lis net.Listener){
	for{
		conn,err := lis.Accept()
		if err != nil{
			log.Println("rpc server:accept error",err)
			return
		}
		go server.ServeConn(conn)
	}
}

// default accept
func Accept(lis net.Listener){DefaultServer.Accept(lis)}

// serve implementation
func(server *Server)ServeConn(conn io.ReadWriteCloser){
	defer func(){_ = conn.Close()}()
	// parse option first
	var opt Option
	if err := json.NewDecoder(conn).Decode(&opt);err != nil{
		log.Print("rpc server:option error:",err)
		return
	}
	// if the magic number is wrong, this is not a valid go rpc call
	if opt.MagicNumber != MagicNumber {
		log.Printf("rpc :server: invalid magic number %x",opt.MagicNumber)
		return
	}

	// accoring the the codec type chosen by client, we generate codec.
	f := codec.NewCodecFuncMap[opt.CodecType]

	if f == nil{
		log.Printf("rpc server:invalid codec type %s",opt.CodecType)
		return 
	}
	// let codec handle rest of the connection, the connection will be passed into codec in constructor
	server.serveCodec(f(conn))

}

var invalidRequest = struct{}{}
// from here there three process
// 1. reanding request
// 2. handle request
// 3. send response
func(server *Server)serveCodec(cc codec.Codec){
	sending := new(sync.Mutex) // we want to send complete response
	wg := new(sync.WaitGroup) // wait until all requests are handled
	// start to serve request
	for{
		// read request and handle error until error occurs.
		req, err := server.readRequest(cc)
		if err != nil{
			if req == nil{
				break // cannot recover, close the connection
			}
			// send request must be sequential or client cannot tell, so we have mutex lock "sending"
			req.h.Error = err.Error()
			server.sendResponse(cc,req.h,invalidRequest,sending)
			continue
		}
		// handle request can be concurrent
		wg.Add(1)
		go server.handleRequest(cc,req,sending,wg)
	}
	// wailt until all request has been handle
	wg.Wait()
	_ = cc.Close()

}


// create a request struct
type request struct{
	h *codec.Header
	argv, replyv reflect.Value
}


// read the request header first
func(server *Server)readRequestHeader(cc codec.Codec)(*codec.Header,error){
	var h codec.Header
	if err := cc.ReadHeader(&h);err != nil{
		if err != io.EOF && err != io.ErrUnexpectedEOF{
			log.Println("RPC server:read header error:",err)
		}
		return nil, err
	}
	return &h,nil
}

// method to read entire request
func(server *Server)readRequest(cc codec.Codec)(*request,error){
	// read header
	h, err := server.readRequestHeader(cc)
	if err != nil{
		return nil, err
	}
	// create request
	req := &request{h:h}
	// TODO: we will handle type of argument later
	// read argv
	req.argv = reflect.New(reflect.TypeOf(""))
	// read request
	if err = cc.ReadBody(req.argv.Interface());err != nil{
		log.Println("rpc server:read argv err:",err)
	}
	return req,nil
	
}

// sending response is a sequential process
func(server *Server)sendResponse(cc codec.Codec,h *codec.Header, body interface{}, sending *sync.Mutex){
	sending.Lock()
	defer sending.Unlock()
	if err := cc.Write(h,body);err != nil{
		log.Println("rpc server: write response error")
	}
}


func(server *Server)handleRequest(cc codec.Codec, req *request, sending *sync.Mutex, wg *sync.WaitGroup){
	// TODO, should call registered rpc methods to get the right replyv
	// just print argv and send a hello message
	defer wg.Done()
	log.Println(req.h, req.argv.Elem())
	req.replyv = reflect.ValueOf(fmt.Sprintf("geerpc resp %d", req.h.Seq))
	server.sendResponse(cc, req.h, req.replyv.Interface(), sending)
}

