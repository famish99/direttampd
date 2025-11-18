#ifndef LIB_MEMORY_PLAY_CONTROLLER_H
#define LIB_MEMORY_PLAY_CONTROLLER_H

#ifdef __cplusplus
extern "C" {
#endif

#include <stdint.h>
#include <stddef.h>
#include <stdbool.h>

// Error codes
typedef enum {
    MPC_SUCCESS = 0,
    MPC_ERROR_SOCKET_OPEN = -1,
    MPC_ERROR_FIND_TARGET = -2,
    MPC_ERROR_NO_HOSTS_FOUND = -3,
    MPC_ERROR_INVALID_PARAM = -4,
    MPC_ERROR_CONNECTION = -5,
    MPC_ERROR_TIMEOUT = -6,
    MPC_ERROR_MEMORY = -7,
    MPC_ERROR_UNKNOWN = -99
} MPCErrorCode;

// Structure to hold information about a discovered MemoryPlayHost
typedef struct {
    char ip_address[64];        // IPv6 address string (e.g., "fe80::1234:5678:9abc:def0")
    uint32_t interface_number;  // Network interface number
    char target_name[256];      // Name of the target device
    char output_name[256];      // Name of the output
    bool is_loopback;          // True if this is a loopback address
} MPCHostInfo;

// Structure to hold a list of discovered hosts
typedef struct {
    MPCHostInfo* hosts;         // Array of host information
    size_t count;              // Number of hosts in the array
} MPCHostList;

// Structure to hold information about a Diretta target device
typedef struct {
    char ip_address[64];        // IPv6 address string
    uint32_t interface_number;  // Network interface number
    char target_name[256];      // Name of the target device
} MPCTargetInfo;

// Structure to hold a list of Diretta targets
typedef struct {
    MPCTargetInfo* targets;     // Array of target information
    size_t count;              // Number of targets in the array
} MPCTargetList;

// Structure to hold a single tag
typedef struct {
    char tag[512];             // Tag string (format: "INDEX:TIME:NAME")
} MPCTagInfo;

// Structure to hold a list of tags
typedef struct {
    MPCTagInfo* tags;          // Array of tag information
    size_t count;              // Number of tags in the array
} MPCTagList;

// Library configuration options
typedef struct {
    bool enable_logging;       // Enable/disable logging output
    bool verbose_mode;         // Enable verbose/debug logging
} MPCConfig;

// Opaque handles for C++ objects
typedef void* MPCWavHandle;
typedef void* MPCFormatHandle;
typedef void* MPCSessionHandle;

// Playback status enum
typedef enum {
    MPC_STATUS_DISCONNECTED = 0,
    MPC_STATUS_PLAYING = 1,
    MPC_STATUS_PAUSED = 2
} MPCPlaybackStatus;

/**
 * Initialize the library with configuration options.
 * This should be called before any other library functions.
 *
 * @param config Configuration options (can be NULL for defaults)
 * @return MPC_SUCCESS on success, error code on failure
 */
int mpc_init(const MPCConfig* config);

/**
 * Cleanup and release any resources used by the library.
 */
void mpc_cleanup(void);

/**
 * Discover and list all available MemoryPlayHost instances on the network.
 *
 * @param host_list Pointer to receive the list of discovered hosts.
 *                  The caller must free this with mpc_free_host_list().
 * @return MPC_SUCCESS on success, error code on failure
 */
int mpc_list_hosts(MPCHostList** host_list);

/**
 * Free a host list previously returned by mpc_list_hosts().
 *
 * @param host_list The host list to free
 */
void mpc_free_host_list(MPCHostList* host_list);

/**
 * List available Diretta targets from a connected MemoryPlayHost.
 *
 * @param host_address IPv6 address of the host (e.g., "::1" or "fe80::1234:5678:9abc:def0")
 * @param interface_number Network interface number
 * @param target_list Pointer to receive the list of discovered targets.
 *                    The caller must free this with mpc_free_target_list().
 * @return MPC_SUCCESS on success, error code on failure
 */
int mpc_list_targets(const char* host_address, uint32_t interface_number, MPCTargetList** target_list);

/**
 * Free a target list previously returned by mpc_list_targets().
 *
 * @param target_list The target list to free
 */
void mpc_free_target_list(MPCTargetList* target_list);

/**
 * Open a WAV/FLAC/DSF/DFF/AIFF audio file.
 *
 * @param filename Path to the audio file
 * @param handle Pointer to receive the WAV handle
 * @return MPC_SUCCESS on success, error code on failure
 */
int mpc_wav_open(const char* filename, MPCWavHandle* handle);

/**
 * Close a WAV file and release resources.
 *
 * @param handle The WAV handle to close
 */
void mpc_wav_close(MPCWavHandle handle);

/**
 * Get the audio format from a WAV file.
 *
 * @param handle The WAV handle
 * @param format Pointer to receive the format handle
 * @return MPC_SUCCESS on success, error code on failure
 */
int mpc_wav_get_format(MPCWavHandle handle, MPCFormatHandle* format);

/**
 * Get the title/metadata from a WAV file.
 *
 * @param handle The WAV handle
 * @return Title string (valid until handle is closed, do not free)
 */
const char* mpc_wav_get_title(MPCWavHandle handle);

/**
 * Get the track index from a WAV file.
 *
 * @param handle The WAV handle
 * @return Track index number
 */
int mpc_wav_get_index(MPCWavHandle handle);

/**
 * Upload audio files to a MemoryPlayHost.
 *
 * @param host_address IPv6 address of the host
 * @param interface_number Network interface number
 * @param wav_handles Array of WAV file handles (must all have same format)
 * @param wav_count Number of WAV files in the array
 * @param format Format handle from one of the WAV files
 * @param loop_mode If true, enable loop playback
 * @return MPC_SUCCESS on success, error code on failure
 */
int mpc_upload_audio(const char* host_address,
                     uint32_t interface_number,
                     MPCWavHandle* wav_handles,
                     size_t wav_count,
                     MPCFormatHandle format,
                     bool loop_mode);

/**
 * Create a control session to a MemoryPlayHost.
 * This establishes a persistent connection for sending control commands.
 *
 * @param host_address IPv6 address of the host
 * @param interface_number Network interface number
 * @param session Pointer to receive the session handle
 * @return MPC_SUCCESS on success, error code on failure
 */
int mpc_session_create(const char* host_address,
                       uint32_t interface_number,
                       MPCSessionHandle* session);

/**
 * Close a control session and release resources.
 *
 * @param session The session handle to close
 */
void mpc_session_close(MPCSessionHandle session);

/**
 * Connect the session to a specific Diretta target device.
 *
 * @param session The session handle
 * @param target_address IPv6 address of the target
 * @param interface_number Network interface number of the target
 * @return MPC_SUCCESS on success, error code on failure
 */
int mpc_session_connect_target(MPCSessionHandle session,
                                const char* target_address,
                                uint32_t interface_number);

/**
 * Start or resume playback.
 *
 * @param session The session handle
 * @return MPC_SUCCESS on success, error code on failure
 */
int mpc_session_play(MPCSessionHandle session);

/**
 * Pause playback.
 *
 * @param session The session handle
 * @return MPC_SUCCESS on success, error code on failure
 */
int mpc_session_pause(MPCSessionHandle session);

/**
 * Seek forward or backward by a number of seconds.
 *
 * @param session The session handle
 * @param offset_seconds Number of seconds to seek (positive = forward, negative = backward)
 * @return MPC_SUCCESS on success, error code on failure
 */
int mpc_session_seek(MPCSessionHandle session, int64_t offset_seconds);

/**
 * Seek to the beginning of the playlist/track.
 *
 * @param session The session handle
 * @return MPC_SUCCESS on success, error code on failure
 */
int mpc_session_seek_to_start(MPCSessionHandle session);

/**
 * Seek to an absolute position in seconds.
 *
 * @param session The session handle
 * @param position_seconds Absolute position in seconds from the start
 * @return MPC_SUCCESS on success, error code on failure
 */
int mpc_session_seek_absolute(MPCSessionHandle session, int64_t position_seconds);

/**
 * Stop playback and disconnect from target.
 *
 * @param session The session handle
 * @return MPC_SUCCESS on success, error code on failure
 */
int mpc_session_quit(MPCSessionHandle session);

/**
 * Get the current playback status.
 *
 * @param session The session handle
 * @param status Pointer to receive the playback status
 * @return MPC_SUCCESS on success, error code on failure
 */
int mpc_session_get_play_status(MPCSessionHandle session, MPCPlaybackStatus* status);

/**
 * Get the current playback time in seconds.
 *
 * @param session The session handle
 * @param time_seconds Pointer to receive the current time in seconds
 * @return MPC_SUCCESS on success, error code on failure
 */
int mpc_session_get_current_time(MPCSessionHandle session, int64_t* time_seconds);

/**
 * Get the list of tags from the current playlist.
 *
 * @param session The session handle
 * @param tag_list Pointer to receive the list of tags.
 *                 The caller must free this with mpc_free_tag_list().
 * @return MPC_SUCCESS on success, error code on failure
 */
int mpc_session_get_tag_list(MPCSessionHandle session, MPCTagList** tag_list);

/**
 * Free a tag list previously returned by mpc_session_get_tag_list().
 *
 * @param tag_list The tag list to free
 */
void mpc_free_tag_list(MPCTagList* tag_list);

/**
 * Get a human-readable error message for an error code.
 *
 * @param error_code The error code
 * @return A string describing the error (do not free this)
 */
const char* mpc_error_string(int error_code);

#ifdef __cplusplus
}
#endif

#endif // LIB_MEMORY_PLAY_CONTROLLER_H
