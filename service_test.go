package gorpc

import (
	"fmt"
	"reflect"
	"testing"
)

type Foo int 

type Args struct{Num1, Num2 int}

// test exported method
func(f Foo)Sum(args Args,reply *int)error{
	*reply = args.Num1 + args.Num2
	return nil
}


// test not exported method - should not be register as an service
func(f Foo)sum(args Args,reply *int)error{
	*reply = args.Num1 + args.Num2
	return nil
}

func _assert(condition bool, msg string, v ...interface{}) {
	if !condition {
		panic(fmt.Sprintf("assertion failed: "+msg, v...))
	}
}

func TestNewService(t *testing.T) {
	var foo Foo
	s := newService(&foo)
	 _assert(len(s.method) == 1,"wrong service method ,expect 1 but got %d",len(s.method))

	 mType := s.method["Sum"]
	 _assert(mType != nil,"Wrong method, Sum should not be nil")
}

func TestMethodType_Call(t *testing.T) {
	var foo Foo
	s := newService(&foo)
	 mType := s.method["Sum"]

	 argv := mType.newArgv()
	 replyv := mType.newReplyv()

	 argv.Set(reflect.ValueOf(Args{Num1: 1,Num2: 2}))
	 err := s.call(mType,argv,replyv)
	 
	 _assert(err == nil && *replyv.Interface().(*int) == 3 && mType.NumCalls() == 1,"Function is not returning correct value")
}

