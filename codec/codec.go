package codec

import "io"

// we define a header
type Header struct{
	ServiceMethod string // format  "Service.Method"
	Seq uint64 // sequence number chosen by client
	Error string
}
// Codec interface 
type Codec interface{
	io.Closer
	ReadHeader(*Header) error
	ReadBody(interface{})error
	Write(*Header,interface{})error
}

// We use NewCoder func for the factory method
type NewCodecFunc func(io.ReadWriteCloser)Codec

type Type string

// currently we only support gob, json
const(
	GobType Type = "application/gob"
	JsonTYpe Type = "application/json"
)

var NewCodecFuncMap map[Type]NewCodecFunc

func init(){
	NewCodecFuncMap = make(map[Type]NewCodecFunc)
	NewCodecFuncMap[GobType] = NewGobCodec
}



