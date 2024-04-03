package wsgo

import (
	"log"
	// "strconv"
	// "strings"
	"sync"
	// "time"
	"unsafe"

	"github.com/gorilla/websocket"
)

/*
#include <Python.h>

struct websocket_message {
	PyObject* channel;
	PyObject* message;
	int messageType;
};
*/
import "C"

type Websocket struct {
	conn     *websocket.Conn
	channels []string
}

type WebsocketMessage struct {
	channel string
	message []byte
	messageType int
}

var websocketChannels map[string][]*Websocket
var websocketChannelsMutex sync.Mutex
var websocketReadQueue chan WebsocketMessage

func init() {
	websocketChannels = make(map[string][]*Websocket)
	websocketReadQueue = make(chan WebsocketMessage, 1000)
}



var upgrader = websocket.Upgrader{} // use default options

func StartWebsocket(job *RequestJob) {
	c, err := upgrader.Upgrade(job.w.writer, job.req, nil)
	if err != nil {
		log.Print("Couldn't upgrade websocket:", err)
		return
	}

	channels := splitOnCommas(job.w.Header().Get("X-WSGo-Websocket"))
	if len(channels) == 0 {
		log.Print("No channels specified for websocket")
		return
	}

	ws := &Websocket{
		conn: c,
		channels: channels,
	}

	websocketChannelsMutex.Lock()
	for _, channel := range channels {
		websocketChannels[channel] = append(websocketChannels[channel], ws)
	}
	websocketChannelsMutex.Unlock()

	defer c.Close()
	for {
		mt, message, err := c.ReadMessage()
		if err != nil {
			log.Println("read:", err)
			break
		}

		websocketReadQueue <- WebsocketMessage{
			// should we return just the first one, or the whole string?
			channel: channels[0],
			message: message,
			messageType: mt,
		}

		// log.Printf("recv: %s, type: %s", message, mt)
		// err = c.WriteMessage(mt, message)
		// if err != nil {
		// 	log.Println("write:", err)
		// 	break
		// }
	}
}

//export go_read_websockets
func go_read_websockets() C.struct_websocket_message {
	// release the GIL
	gilState := C.PyThreadState_Get()
	C.PyEval_SaveThread()

	msg := <-websocketReadQueue

	// regrab the GIL
	C.PyEval_RestoreThread(gilState)

	// avoid a copy by passing Go memory to C
	c := ([]byte)(msg.channel)
	channel := C.PyUnicode_FromStringAndSize(
	 	(*C.char)(unsafe.Pointer(&c[0])),
	 	C.long(len(msg.channel)),
	)
	message := C.PyBytes_FromStringAndSize(
		(*C.char)(unsafe.Pointer(&msg.message[0])),
		C.long(len(msg.message)),
	)

	return C.struct_websocket_message{
		channel: channel,
		message: message,
		messageType: C.int(msg.messageType),
	}
}

//export go_send_websockets
func go_send_websockets(msg C.struct_websocket_message) {
	var csize C.Py_ssize_t
	c_channel := C.PyUnicode_AsUTF8AndSize(msg.channel, &csize)
	if c_channel == nil {
		C.PyErr_Print()
		return
	}

	var c_message *C.char
	var msize C.Py_ssize_t

	ret := C.PyBytes_AsStringAndSize(msg.message, &c_message, &msize)
	if ret == -1 {
		C.PyErr_Print()
		return
	}

	channel := C.GoStringN(c_channel, C.int(csize))
	message := C.GoBytes(unsafe.Pointer(c_message), C.int(msize))

	//v := unsafe.Slice((*byte)(unsafe.Pointer(msg.channel)), size)

	channels := splitOnCommas(channel)

	// release the GIL
	gilState := C.PyThreadState_Get()
	C.PyEval_SaveThread()

	for _, c := range channels {
		for _, ws := range websocketChannels[c] {
			err := ws.conn.WriteMessage((int)(msg.messageType), message)
			if err != nil {
				log.Println("write:", err)
				//break
			}
		}
	}

	// regrab the GIL
	C.PyEval_RestoreThread(gilState)
}