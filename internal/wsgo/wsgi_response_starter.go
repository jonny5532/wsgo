package wsgo

import (
	"strconv"
	"sync"
	"unsafe"
)

import "C"

type ResponseStart struct {
	status         int
	status_message string
	headers        map[string][]string
}

var responseStarts map[int64]ResponseStart
var responseStartsMutex sync.Mutex

func init() {
	responseStarts = make(map[int64]ResponseStart)
}

func GetWsgiResponseStart(requestId int64) *ResponseStart {
	responseStartsMutex.Lock()
	response_start := responseStarts[requestId]
	delete(responseStarts, requestId)
	responseStartsMutex.Unlock()
	return &response_start
}

//export go_wsgi_start_response
func go_wsgi_start_response(request_id C.long, status *C.char, status_length C.int, header_parts **C.char, header_part_lengths *C.int, headers_size C.int) {
	statusLine := C.GoStringN(status, status_length)
	if len(statusLine) < 5 || statusLine[3] != ' ' {
		return
	}
	statusCode, err := strconv.Atoi(statusLine[0:3])
	if err != nil {
		return
	}

	rs := ResponseStart{
		status:         statusCode,
		status_message: statusLine[4:],
		headers:        make(map[string][]string),
	}

	for i := 0; i < int(headers_size); i++ {
		k_ptr := (*(**C.char)(unsafe.Pointer(uintptr(unsafe.Pointer(header_parts)) + (uintptr(i*2) * unsafe.Sizeof(*header_parts)))))
		k_len := (*(*C.int)(unsafe.Pointer(uintptr(unsafe.Pointer(header_part_lengths)) + (uintptr(i*2) * unsafe.Sizeof(*header_part_lengths)))))
		k := C.GoStringN(k_ptr, k_len)

		v_ptr := (*(**C.char)(unsafe.Pointer(uintptr(unsafe.Pointer(header_parts)) + (uintptr(1+i*2) * unsafe.Sizeof(*header_parts)))))
		v_len := (*(*C.int)(unsafe.Pointer(uintptr(unsafe.Pointer(header_part_lengths)) + (uintptr(1+i*2) * unsafe.Sizeof(*header_part_lengths)))))
		v := C.GoStringN(v_ptr, v_len)

		rs.headers[k] = append(rs.headers[k], v)
	}

	responseStartsMutex.Lock()
	responseStarts[int64(request_id)] = rs
	responseStartsMutex.Unlock()

}
