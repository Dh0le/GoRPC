package gorpc

import (
	"go/ast"
	"log"
	"reflect"
	"sync/atomic"
)

// method type contains a full information of a method
type methodType struct{
	method reflect.Method // method name
	ArgType reflect.Type // argument type
	ReplyType reflect.Type // return vla type
	numCalls uint64 // we can static the number of calls
}

func(m *methodType)NumCalls()uint64{
	return atomic.LoadUint64(&m.numCalls)
}

// internal method to create new method type
func(m *methodType)newArgv()reflect.Value{
	var argv reflect.Value
	// different in creating pointer type or object type
	if m.ArgType.Kind() == reflect.Ptr{
		argv = reflect.New(m.ArgType.Elem())
	}else{
		argv = reflect.New(m.ArgType).Elem()
	}
	return argv
}

// internal method to create new method type
func(m *methodType)newReplyv()reflect.Value{
	replyv := reflect.New(m.ReplyType.Elem())

	switch m.ReplyType.Elem().Kind(){
	case reflect.Map:
		replyv.Elem().Set(reflect.MakeMap(m.ReplyType.Elem()))
	case reflect.Slice:
		replyv.Elem().Set(reflect.MakeSlice(m.ReplyType.Elem(),0,0))
	}
	return replyv
}

// each struct will create a service
type service struct{
	name string // name of the struct
	typ reflect.Type // type of the struct
	rcvr reflect.Value // object itself
	method map[string]*methodType // every function of the struct
}

// create a new service
func newService(rcvr interface{})*service{
	s := new(service)

	s.rcvr = reflect.ValueOf(rcvr)
	s.name = reflect.Indirect(s.rcvr).Type().Name()

	s.typ = reflect.TypeOf(rcvr)
	if !ast.IsExported(s.name){
		log.Fatalf("rpc server: %s is not a valid service name",s.name)
	}
	// register all method into service
	s.registerMethod()
	return s
}

// register method into service
func(s* service)registerMethod(){
	s.method = make(map[string]*methodType)
	for i := 0;i < s.typ.NumMethod();i++{
		method := s.typ.Method(i)
		mType := method.Type
		// check every method in current struct
		// in reflect first in is itself
		if mType.NumIn() != 3 || mType.NumOut() != 1{
			continue;
		}
		// one out must be error
		if mType.Out(0) != reflect.TypeOf((*error)(nil)).Elem(){
			continue;
		}
		// two arg, both with built in type of exported type
		argType, replyType := mType.In(1), mType.In(2)
		if !isExportedOrBuiltInType(argType) || ! isExportedOrBuiltInType(replyType){
			continue
		}
		s.method[method.Name] = &methodType{
			method:    method,
			ArgType:   argType,
			ReplyType: replyType,
		}
		log.Printf("rpc server: register %s.%s\n", s.name, method.Name)
	}
}

func isExportedOrBuiltInType(t reflect.Type)bool{
	return ast.IsExported(t.Name()) || t.PkgPath() == ""
}

// this funtion will allow serivce to execute the function
func(s *service)call(m *methodType, argv,replyv reflect.Value)error{
	atomic.AddUint64(&m.numCalls,1)

	f := m.method.Func
	returnValues := f.Call([]reflect.Value{s.rcvr,argv,replyv})

	if errInter := returnValues[0].Interface();errInter != nil{
		return errInter.(error)
	}
	return nil
}







