package gorpc

import (
	"encoding/json"
	"errors"
	"fmt"
	"gorpc/codec"
	"io"
	"log"
	"net"
	"reflect"
	"strings"
	"sync"
	"time"
)

const MagicNumber = 0x3bef5c

// Options should be contained in the meta data arrived first in the connection
type Option struct{
	MagicNumber int // we use this magic number to mark a go rpc request
	CodecType codec.Type // client may use different type of encoding
	ConnectTimeout time.Duration
	HandleTimeout time.Duration
}


var DefaultOption = &Option{
	MagicNumber: MagicNumber,
	CodecType: codec.GobType,
	ConnectTimeout: time.Second*10,// default 10s to connect
	// not hanle time out
}

// RPC server
type Server struct{
	serviceMap sync.Map
}

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
	mtype *methodType
	svc *service
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
	// according to request method string
	// find service and method
	req.svc,req.mtype,err = server.findService(h.ServiceMethod)
	if err != nil{
		return req,err
	}
	// get argument
	req.argv = req.mtype.newArgv()
	// get return
	req.replyv = req.mtype.newReplyv()
	argvi := req.argv.Interface()
	// if arguement is not a pointer
	if req.argv.Type().Kind() != reflect.Ptr{
		argvi = req.argv.Addr().Interface()
	}
	// read request
	if err = cc.ReadBody(argvi);err != nil{
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


func(server *Server)handleRequest(cc codec.Codec, req *request, sending *sync.Mutex, wg *sync.WaitGroup,timeout time.Duration){
	defer wg.Done()
	called := make(chan struct{})
	sent := make(chan struct{})
	// use a goroutine to run call
	go func(){
		err :=  req.svc.call(req.mtype,req.argv,req.replyv)
		// place a holder into channel indicate call is completed
		called <- struct{}{}
		if err != nil{
			req.h.Error = err.Error()
			server.sendResponse(cc,req.h,invalidRequest,sending)
			// place a holder into cahnnel indicate sent is completed
			sent <- struct{}{}
			return
		}
	}()

	// if not timeout
	if timeout == 0{
		<-called
		<-sent
		return
	}
	// check if timeout reach frist or called completed first
	select{
	case <-time.After(timeout):
		req.h.Error = fmt.Sprintf("rpc server: request handle time out:expect within %s",timeout)
		server.sendResponse(cc,req.h,invalidRequest,sending)
	case <-called:
		<-sent
	}
	
}


func(server *Server)Register(rcvr interface{})error{
	s := newService(rcvr)

	if _, dup := server.serviceMap.LoadOrStore(s.name,s);dup{
		return errors.New("rpc server: service already registered")
	}
	return nil
}

func Register(rcvr interface{})error{return DefaultServer.Register(rcvr)}

// given a service method string find serice and method
func(server *Server)findService(serviceMethod string)(svc *service,mType *methodType,err error){
	// split string
	dot := strings.LastIndex(serviceMethod,".")
	if dot < 0{
		err= errors.New("rpc server: service/method request is wrong formed")
		return
	}
	// get name
	serviceName,methodName := serviceMethod[:dot],serviceMethod[dot+1:]
	//load service
	svic,ok := server.serviceMap.Load(serviceName)
	if !ok{
		err = errors.New("rpc server: cannot find service")
		return
	}
	// parse service
	svc = svic.(*service)
	// get method
	mType = svc.method[methodName]

	if mType == nil{
		err = errors.New("rpc server:cannot find method")
	}
	return 
}





