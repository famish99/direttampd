package memoryplay

/*
#cgo CFLAGS: -I${SRCDIR}/../../MemoryPlayController
#cgo linux,amd64 LDFLAGS: ${SRCDIR}/../../MemoryPlayController/libmemoryplaycontroller.a ${SRCDIR}/../../MemoryPlayController/libDirettaHost_x64-linux-15v2.a ${SRCDIR}/../../MemoryPlayController/libACQUA_x64-linux-15v2.a -lstdc++ -lm -lpthread
#cgo linux,arm64 LDFLAGS: ${SRCDIR}/../../MemoryPlayController/libmemoryplaycontroller.a ${SRCDIR}/../../MemoryPlayController/libDirettaHost_aarch64-linux-15.a ${SRCDIR}/../../MemoryPlayController/libACQUA_aarch64-linux-15.a -lstdc++ -lm -lpthread
#include "lib_memory_play_controller.h"
#include <stdlib.h>
*/
import "C"
import (
	"fmt"
	"log"
	"sync"
	"unsafe"
)

// Session represents a control session to a MemoryPlay host
type Session struct {
	handle          C.MPCSessionHandle
	hostAddress     string
	interfaceNumber uint32
	mu              sync.Mutex
}

// CreateSession creates a control session
func CreateSession(hostAddress string, interfaceNumber uint32) (*Session, error) {
	cHostAddr := C.CString(hostAddress)
	defer C.free(unsafe.Pointer(cHostAddr))

	var handle C.MPCSessionHandle
	ret := C.mpc_session_create(cHostAddr, C.uint32_t(interfaceNumber), &handle)
	if ret != C.MPC_SUCCESS {
		return nil, fmt.Errorf("mpc_session_create failed: %s", C.GoString(C.mpc_error_string(ret)))
	}

	return &Session{
		handle:          handle,
		hostAddress:     hostAddress,
		interfaceNumber: interfaceNumber,
	}, nil
}

// Close closes the session
func (s *Session) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.handle != nil {
		C.mpc_session_close(s.handle)
		s.handle = nil
	}
}

// isConnectionError checks if the error code indicates a connection/timeout error
func isConnectionError(ret C.int) bool {
	return ret == C.MPC_ERROR_CONNECTION || ret == C.MPC_ERROR_TIMEOUT
}

// recreateSession attempts to recreate the session after a connection error
func (s *Session) recreateSession() error {
	// Close old handle if it exists
	if s.handle != nil {
		C.mpc_session_close(s.handle)
		s.handle = nil
	}

	// Create new session
	cHostAddr := C.CString(s.hostAddress)
	defer C.free(unsafe.Pointer(cHostAddr))

	var handle C.MPCSessionHandle
	ret := C.mpc_session_create(cHostAddr, C.uint32_t(s.interfaceNumber), &handle)
	if ret != C.MPC_SUCCESS {
		return fmt.Errorf("failed to recreate session: %s", C.GoString(C.mpc_error_string(ret)))
	}

	s.handle = handle
	log.Printf("Session recreated after connection error")
	return nil
}

// ConnectTarget connects to a specific Diretta target
func (s *Session) ConnectTarget(targetAddress string, interfaceNumber uint32) error {
	cTargetAddr := C.CString(targetAddress)
	defer C.free(unsafe.Pointer(cTargetAddr))

	ret := C.mpc_session_connect_target(s.handle, cTargetAddr, C.uint32_t(interfaceNumber))
	if ret != C.MPC_SUCCESS {
		return fmt.Errorf("mpc_session_connect_target failed: %s", C.GoString(C.mpc_error_string(ret)))
	}
	return nil
}

// Play starts or resumes playback
func (s *Session) Play() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ret := C.mpc_session_play(s.handle)
	if isConnectionError(ret) {
		log.Printf("Connection error during Play, attempting to recreate session")
		if err := s.recreateSession(); err != nil {
			return fmt.Errorf("mpc_session_play failed: %s (reconnect failed: %w)", C.GoString(C.mpc_error_string(ret)), err)
		}
		// Retry once after reconnection
		ret = C.mpc_session_play(s.handle)
	}

	if ret != C.MPC_SUCCESS {
		return fmt.Errorf("mpc_session_play failed: %s", C.GoString(C.mpc_error_string(ret)))
	}
	return nil
}

// Pause pauses playback
func (s *Session) Pause() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ret := C.mpc_session_pause(s.handle)
	if isConnectionError(ret) {
		log.Printf("Connection error during Pause, attempting to recreate session")
		if err := s.recreateSession(); err != nil {
			return fmt.Errorf("mpc_session_pause failed: %s (reconnect failed: %w)", C.GoString(C.mpc_error_string(ret)), err)
		}
		// Retry once after reconnection
		ret = C.mpc_session_pause(s.handle)
	}

	if ret != C.MPC_SUCCESS {
		return fmt.Errorf("mpc_session_pause failed: %s", C.GoString(C.mpc_error_string(ret)))
	}
	return nil
}

// Seek seeks forward or backward by seconds
func (s *Session) Seek(offsetSeconds int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ret := C.mpc_session_seek(s.handle, C.int64_t(offsetSeconds))
	if isConnectionError(ret) {
		log.Printf("Connection error during Seek, attempting to recreate session")
		if err := s.recreateSession(); err != nil {
			return fmt.Errorf("mpc_session_seek failed: %s (reconnect failed: %w)", C.GoString(C.mpc_error_string(ret)), err)
		}
		// Retry once after reconnection
		ret = C.mpc_session_seek(s.handle, C.int64_t(offsetSeconds))
	}

	if ret != C.MPC_SUCCESS {
		return fmt.Errorf("mpc_session_seek failed: %s", C.GoString(C.mpc_error_string(ret)))
	}
	return nil
}

// SeekToStart seeks to the beginning
func (s *Session) SeekToStart() error {
	ret := C.mpc_session_seek_to_start(s.handle)
	if ret != C.MPC_SUCCESS {
		return fmt.Errorf("mpc_session_seek_to_start failed: %s", C.GoString(C.mpc_error_string(ret)))
	}
	return nil
}

// SeekAbsolute seeks to an absolute position in seconds
func (s *Session) SeekAbsolute(positionSeconds int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ret := C.mpc_session_seek_absolute(s.handle, C.int64_t(positionSeconds))
	if isConnectionError(ret) {
		log.Printf("Connection error during SeekAbsolute, attempting to recreate session")
		if err := s.recreateSession(); err != nil {
			return fmt.Errorf("mpc_session_seek_absolute failed: %s (reconnect failed: %w)", C.GoString(C.mpc_error_string(ret)), err)
		}
		// Retry once after reconnection
		ret = C.mpc_session_seek_absolute(s.handle, C.int64_t(positionSeconds))
	}

	if ret != C.MPC_SUCCESS {
		return fmt.Errorf("mpc_session_seek_absolute failed: %s", C.GoString(C.mpc_error_string(ret)))
	}
	return nil
}

// Quit stops playback and disconnects
func (s *Session) Quit() error {
	ret := C.mpc_session_quit(s.handle)
	if ret != C.MPC_SUCCESS {
		return fmt.Errorf("mpc_session_quit failed: %s", C.GoString(C.mpc_error_string(ret)))
	}
	return nil
}

// PlaybackStatus represents playback status
type PlaybackStatus int

const (
	StatusDisconnected PlaybackStatus = 0
	StatusPlaying      PlaybackStatus = 1
	StatusPaused       PlaybackStatus = 2
)

// GetPlayStatus returns current playback status
func (s *Session) GetPlayStatus() (PlaybackStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var status C.MPCPlaybackStatus
	ret := C.mpc_session_get_play_status(s.handle, &status)
	if isConnectionError(ret) {
		log.Printf("Connection error during GetPlayStatus, attempting to recreate session")
		if err := s.recreateSession(); err != nil {
			return StatusDisconnected, fmt.Errorf("mpc_session_get_play_status failed: %s (reconnect failed: %w)", C.GoString(C.mpc_error_string(ret)), err)
		}
		// Retry once after reconnection
		ret = C.mpc_session_get_play_status(s.handle, &status)
	}

	if ret != C.MPC_SUCCESS {
		return StatusDisconnected, fmt.Errorf("mpc_session_get_play_status failed: %s", C.GoString(C.mpc_error_string(ret)))
	}
	return PlaybackStatus(status), nil
}

// GetCurrentTime returns current playback time in seconds
func (s *Session) GetCurrentTime() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var timeSeconds C.int64_t
	ret := C.mpc_session_get_current_time(s.handle, &timeSeconds)
	if isConnectionError(ret) {
		log.Printf("Connection error during GetCurrentTime, attempting to recreate session")
		if err := s.recreateSession(); err != nil {
			return -1, fmt.Errorf("mpc_session_get_current_time failed: %s (reconnect failed: %w)", C.GoString(C.mpc_error_string(ret)), err)
		}
		// Retry once after reconnection
		ret = C.mpc_session_get_current_time(s.handle, &timeSeconds)
	}

	if ret != C.MPC_SUCCESS {
		return -1, fmt.Errorf("mpc_session_get_current_time failed: %s", C.GoString(C.mpc_error_string(ret)))
	}
	return int64(timeSeconds), nil
}

// TagInfo represents a tag entry
type TagInfo struct {
	Tag string // Format: "INDEX:TIME:NAME"
}

// GetTagList returns the list of tags
func (s *Session) GetTagList() ([]TagInfo, error) {
	var tagList *C.MPCTagList
	ret := C.mpc_session_get_tag_list(s.handle, &tagList)
	if ret != C.MPC_SUCCESS {
		return nil, fmt.Errorf("mpc_session_get_tag_list failed: %s", C.GoString(C.mpc_error_string(ret)))
	}
	defer C.mpc_free_tag_list(tagList)

	if tagList == nil || tagList.count == 0 {
		return []TagInfo{}, nil
	}

	tags := make([]TagInfo, tagList.count)
	cTags := (*[1 << 30]C.MPCTagInfo)(unsafe.Pointer(tagList.tags))[:tagList.count:tagList.count]

	for i, ct := range cTags {
		tags[i] = TagInfo{
			Tag: C.GoString(&ct.tag[0]),
		}
	}

	return tags, nil
}
