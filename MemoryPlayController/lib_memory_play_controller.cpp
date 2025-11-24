#include "lib_memory_play_controller.h"

#include <iostream>
#include <string>
#include <cstring>
#include <vector>
#include <sstream>
#include <map>
#include <filesystem>

#include <Diretta/Find>
#include <ACQUA/TCPV6>
#include "lib_memory_play_client.hpp"

#include "lib_wav.hpp"

using namespace std;
using namespace ACQUA;
using namespace DIRETTA;

const int FILL_TIME = 0;

// Global state for the library
static struct {
    bool initialized;
    bool logging_enabled;
    bool verbose_mode;
} g_lib_state = { false, false, false };

// Helper function to receive and process messages with a custom frame handler
template<typename FrameHandler>
static int receiveMessages(
    TCPV6Client& client,
    FrameHandler handleFrame,
    int timeoutMs = 500
) {
    Clock lastRecv = Clock::now();

    while (true) {
        WAIT_CODE waitCode = client.wait(Clock::MilliSeconds(100));

        if (waitCode == WAIT_CODE::ERROR) {
            return MPC_ERROR_CONNECTION;
        }

        if (waitCode == WAIT_CODE::TIMEOUT) {
            if (Clock::now() - lastRecv >= Clock::MilliSeconds(timeoutMs)) {
                return MPC_ERROR_TIMEOUT;
            }
            continue;
        }

        if (waitCode == WAIT_CODE::WAKEUP) {
            MemoryPlayClient::ReceiveMessage receiveBuffer;
            if (!client.receive(receiveBuffer)) {
                return MPC_ERROR_CONNECTION;
            }

            while (receiveBuffer.checkFrame()) {

                if (receiveBuffer.getType() == 1) {
                    MemoryPlayClient::ReceiveMessageFrames frames(receiveBuffer.getFramePayload());
                    for (const auto& frame : frames) {
                        lastRecv = Clock::now();
                        if (handleFrame(frame.first, frame.second)) {
                            return MPC_SUCCESS;
                        }
                    }
                }

                receiveBuffer.next();
            }
        }
    }

    return MPC_SUCCESS;
}

// Session class for persistent control connections
class ControlSession {
public:
    ControlSession() : connected(false) {}

    bool open(const char* host_address, uint32_t interface_number) {
        if (!connectAddress.set_str(host_address)) {
            return false;
        }
        connectAddress.set_ifno(interface_number);

        if (!client.open(true)) {
            return false;
        }

        if (!client.connect(connectAddress)) {
            return false;
        }

        connected = true;
        return true;
    }

    void close() {
        connected = false;
        // Client will close automatically on destruction
    }

    bool is_connected() const {
        return connected;
    }

    int connect_target(const char* target_address, uint32_t interface_number) {
        if (!connected) {
            return MPC_ERROR_CONNECTION;
        }

        MemoryPlayClient::SendMessageFrames header;
        stringstream ss;
        ss << target_address << " " << interface_number;
        header.addHeader("Connect", ss.str());

        if (!client.send(header)) {
            return MPC_ERROR_CONNECTION;
        }

        return MPC_SUCCESS;
    }

    int play() {
        if (!connected) {
            return MPC_ERROR_CONNECTION;
        }

        MemoryPlayClient::SendMessageFrames header;
        header.addHeader("Play", "");

        if (!client.send(header)) {
            return MPC_ERROR_CONNECTION;
        }

        return MPC_SUCCESS;
    }

    int pause() {
        if (!connected) {
            return MPC_ERROR_CONNECTION;
        }

        MemoryPlayClient::SendMessageFrames header;
        header.addHeader("Pause", "");

        if (!client.send(header)) {
            return MPC_ERROR_CONNECTION;
        }

        return MPC_SUCCESS;
    }

    int seek(int64_t offset_seconds) {
        if (!connected) {
            return MPC_ERROR_CONNECTION;
        }

        MemoryPlayClient::SendMessageFrames header;
        stringstream ss;
        if (offset_seconds > 0) {
            ss << "+" << offset_seconds;
        } else {
            ss << offset_seconds;
        }
        header.addHeader("Seek", ss.str());

        if (!client.send(header)) {
            return MPC_ERROR_CONNECTION;
        }

        return MPC_SUCCESS;
    }

    int seek_to_start() {
        if (!connected) {
            return MPC_ERROR_CONNECTION;
        }

        MemoryPlayClient::SendMessageFrames header;
        header.addHeader("Seek", "Front");

        if (!client.send(header)) {
            return MPC_ERROR_CONNECTION;
        }

        return MPC_SUCCESS;
    }

    int seek_absolute(int64_t position_seconds) {
        if (!connected) {
            return MPC_ERROR_CONNECTION;
        }

        MemoryPlayClient::SendMessageFrames header;
        stringstream ss;
        ss << position_seconds;  // No + or - prefix, just the absolute position
        header.addHeader("Seek", ss.str());

        if (!client.send(header)) {
            return MPC_ERROR_CONNECTION;
        }

        return MPC_SUCCESS;
    }

    int quit() {
        if (!connected) {
            return MPC_ERROR_CONNECTION;
        }

        MemoryPlayClient::SendMessageFrames header;
        header.addHeader("Seek", "Quit");

        if (!client.send(header)) {
            return MPC_ERROR_CONNECTION;
        }

        return MPC_SUCCESS;
    }

    int get_play_status(MPCPlaybackStatus* status) {
        if (!connected || !status) {
            return MPC_ERROR_INVALID_PARAM;
        }

        *status = MPC_STATUS_DISCONNECTED;

        // Request status from host
        MemoryPlayClient::SendMessageFrames header;
        header.addHeader("Request", "Status");
        if (!client.send(header)) {
            return MPC_ERROR_CONNECTION;
        }

        // Process status messages
        auto handleStatus = [status](const string& key, const string& value) -> bool {
            if (key == "Status") {
                if (value == "Disconnect") {
                    *status = MPC_STATUS_DISCONNECTED;
                } else if (value == "Play") {
                    *status = MPC_STATUS_PLAYING;
                } else if (value == "Pause") {
                    *status = MPC_STATUS_PAUSED;
                }
                return true;
            }
            return false;
        };

        int result = receiveMessages(client, handleStatus);
        if (result == MPC_ERROR_CONNECTION) {
            connected = false;
            return MPC_ERROR_CONNECTION;
        }

        return MPC_SUCCESS;
    }

    int get_current_time(int64_t* time_seconds) {
        if (!connected || !time_seconds) {
            return MPC_ERROR_INVALID_PARAM;
        }

        *time_seconds = -1;

        // Request status from host
        MemoryPlayClient::SendMessageFrames header;
        header.addHeader("Request", "Status");
        if (!client.send(header)) {
            return MPC_ERROR_CONNECTION;
        }

        // Process time message
        auto handleTime = [time_seconds](const string& key, const string& value) -> bool {
            if (key == "Status") {
                if (value == "Disconnect" || value == "Pause") {
                    return true;
                }
            }
            if (key == "LastTime") {
                *time_seconds = atoll(value.c_str());
                return true;
            }
            return false;
        };

        int result = receiveMessages(client, handleTime, 1250);
        if (result == MPC_ERROR_CONNECTION) {
            connected = false;
            return MPC_ERROR_CONNECTION;
        }

        return MPC_SUCCESS;
    }

    int get_tag_list(vector<string>& tags) {
        if (!connected) {
            return MPC_ERROR_CONNECTION;
        }

        tags.clear();

        // Request status from host
        MemoryPlayClient::SendMessageFrames header;
        header.addHeader("Request", "Status");
        if (!client.send(header)) {
            return MPC_ERROR_CONNECTION;
        }

        // Process tag messages
        auto handleTags = [&tags](const string& key, const string& value) -> bool {
            if (key == "Tag") {
                tags.push_back(value);
                return false;
            }
            return true;
        };

        int result = receiveMessages(client, handleTags);
        if (result == MPC_ERROR_CONNECTION) {
            connected = false;
            return MPC_ERROR_CONNECTION;
        }

        return MPC_SUCCESS;
    }

private:

    TCPV6Client client;
    IPAddress connectAddress;
    bool connected;
};

// Initialize the library
int mpc_init(const MPCConfig* config) {
    if (g_lib_state.initialized) {
        return MPC_SUCCESS; // Already initialized
    }

    if (config) {
        g_lib_state.logging_enabled = config->enable_logging;
        g_lib_state.verbose_mode = config->verbose_mode;
    } else {
        // Default configuration
        g_lib_state.logging_enabled = true;
        g_lib_state.verbose_mode = false;
    }

    g_lib_state.initialized = true;
    return MPC_SUCCESS;
}

// Cleanup library resources
void mpc_cleanup(void) {
    g_lib_state.initialized = false;
    g_lib_state.logging_enabled = false;
    g_lib_state.verbose_mode = false;
}

// List available MemoryPlayHost instances
int mpc_list_hosts(MPCHostList** host_list) {
    if (!host_list) {
        return MPC_ERROR_INVALID_PARAM;
    }

    if (!g_lib_state.initialized) {
        // Auto-initialize with defaults
        mpc_init(nullptr);
    }

    // Set up Find configuration
    Find::Setting findSettings;
    findSettings.Name = "MemoryPlayController";
    findSettings.ProductID = 0;
    findSettings.Loopback = true;

    // Create and open the Find socket
    Find find(findSettings);
    if (!find.open()) {
        return MPC_ERROR_SOCKET_OPEN;
    }

    // Find targets
    Find::TargetResalts targetResults;
    Find::PortResalts portResults;

    if (!find.findTarget(targetResults)) {
        return MPC_ERROR_FIND_TARGET;
    }

    if (!find.findTarget(targetResults, portResults, Find::AUDIO_MEMORY)) {
        return MPC_ERROR_FIND_TARGET;
    }

    if (portResults.empty()) {
        return MPC_ERROR_NO_HOSTS_FOUND;
    }

    // Allocate the host list structure
    MPCHostList* list = (MPCHostList*)malloc(sizeof(MPCHostList));
    if (!list) {
        return MPC_ERROR_MEMORY;
    }

    list->count = portResults.size();
    list->hosts = (MPCHostInfo*)calloc(list->count, sizeof(MPCHostInfo));
    if (!list->hosts) {
        free(list);
        return MPC_ERROR_MEMORY;
    }

    // Populate the host list
    size_t idx = 0;
    for (const auto& portResult : portResults) {
        MPCHostInfo* host = &list->hosts[idx];

        // Get IP address string
        string addr_str;
        portResult.first.get_str(addr_str);
        strncpy(host->ip_address, addr_str.c_str(), sizeof(host->ip_address) - 1);
        host->ip_address[sizeof(host->ip_address) - 1] = '\0';

        // Get interface number
        host->interface_number = portResult.first.get_ifno();

        // Get target and output names
        strncpy(host->target_name, portResult.second.targetName.c_str(), sizeof(host->target_name) - 1);
        host->target_name[sizeof(host->target_name) - 1] = '\0';

        strncpy(host->output_name, portResult.second.outputName.c_str(), sizeof(host->output_name) - 1);
        host->output_name[sizeof(host->output_name) - 1] = '\0';

        // Check if loopback
        host->is_loopback = portResult.first.is_loopback();

        idx++;
    }

    *host_list = list;
    return MPC_SUCCESS;
}

// Free a host list
void mpc_free_host_list(MPCHostList* host_list) {
    if (host_list) {
        if (host_list->hosts) {
            free(host_list->hosts);
        }
        free(host_list);
    }
}

// List available Diretta targets from a host
int mpc_list_targets(const char* host_address, uint32_t interface_number, MPCTargetList** target_list) {
    if (!host_address || !target_list) {
        return MPC_ERROR_INVALID_PARAM;
    }

    if (!g_lib_state.initialized) {
        // Auto-initialize with defaults
        mpc_init(nullptr);
    }

    // Parse the host address
    IPAddress connectAddress;
    if (!connectAddress.set_str(host_address)) {
        return MPC_ERROR_INVALID_PARAM;
    }
    connectAddress.set_ifno(interface_number);

    // Open TCP client socket
    TCPV6Client client;
    if (!client.open(true)) {
        return MPC_ERROR_SOCKET_OPEN;
    }

    // Connect to the host
    if (!client.connect(connectAddress)) {
        return MPC_ERROR_CONNECTION;
    }

    // Send target list request (from lines 664-668)
    MemoryPlayClient::SendMessageFrames header;
    header.addHeader("Request", "TargetList");
    if (!client.send(header)) {
        return MPC_ERROR_CONNECTION;
    }

    // Wait for and collect responses using the helper function
    vector<MPCTargetInfo> targets;

    auto handleTargetList = [&targets](const string& key, const string& value) -> bool {
        if (key == "TargetList") {
            // Parse: "IP_ADDRESS IF_NUMBER TARGET_NAME"
            size_t n1 = value.find(' ');
            if (n1 != string::npos) {
                size_t n2 = value.find(' ', n1 + 1);
                if (n2 != string::npos) {
                    MPCTargetInfo target;

                    // Extract IP address
                    string addr = value.substr(0, n1);
                    strncpy(target.ip_address, addr.c_str(), sizeof(target.ip_address) - 1);
                    target.ip_address[sizeof(target.ip_address) - 1] = '\0';

                    // Extract interface number
                    string ifstr = value.substr(n1 + 1, n2 - n1 - 1);
                    target.interface_number = atoi(ifstr.c_str());

                    // Extract target name
                    string name = value.substr(n2 + 1);
                    strncpy(target.target_name, name.c_str(), sizeof(target.target_name) - 1);
                    target.target_name[sizeof(target.target_name) - 1] = '\0';

                    targets.push_back(target);
                }
            }
            return true;
        }
        return false;
    };

    int result = receiveMessages(client, handleTargetList);
    if (result != MPC_SUCCESS) {
        return result;
    }

    // Allocate and populate the result
    MPCTargetList* list = (MPCTargetList*)malloc(sizeof(MPCTargetList));
    if (!list) {
        return MPC_ERROR_MEMORY;
    }

    list->count = targets.size();
    if (list->count > 0) {
        list->targets = (MPCTargetInfo*)calloc(list->count, sizeof(MPCTargetInfo));
        if (!list->targets) {
            free(list);
            return MPC_ERROR_MEMORY;
        }

        for (size_t i = 0; i < list->count; i++) {
            list->targets[i] = targets[i];
        }
    } else {
        list->targets = nullptr;
    }

    *target_list = list;
    return MPC_SUCCESS;
}

// Free a target list
void mpc_free_target_list(MPCTargetList* target_list) {
    if (target_list) {
        if (target_list->targets) {
            free(target_list->targets);
        }
        free(target_list);
    }
}

// WAV file operations
int mpc_wav_open(const char* filename, MPCWavHandle* handle) {
    if (!filename || !handle) {
        return MPC_ERROR_INVALID_PARAM;
    }

    if (!g_lib_state.initialized) {
        mpc_init(nullptr);
    }

    try {
        WAV* wav = new WAV();
        filesystem::path path = (const char8_t*)filename;

        if (!wav->open(path, true)) {
            delete wav;
            return MPC_ERROR_INVALID_PARAM;
        }

        *handle = (MPCWavHandle)wav;
        return MPC_SUCCESS;
    } catch (...) {
        return MPC_ERROR_UNKNOWN;
    }
}

void mpc_wav_close(MPCWavHandle handle) {
    if (handle) {
        WAV* wav = (WAV*)handle;
        wav->close();
        delete wav;
    }
}

int mpc_wav_get_format(MPCWavHandle handle, MPCFormatHandle* format) {
    if (!handle || !format) {
        return MPC_ERROR_INVALID_PARAM;
    }

    try {
        WAV* wav = (WAV*)handle;
        FormatConfigure* fmt = new FormatConfigure(wav->getFormat());
        *format = (MPCFormatHandle)fmt;
        return MPC_SUCCESS;
    } catch (...) {
        return MPC_ERROR_UNKNOWN;
    }
}

void mpc_free_format(MPCFormatHandle format) {
    if (format) {
        FormatConfigure* fmt = (FormatConfigure*)format;
        delete fmt;
    }
}

const char* mpc_wav_get_title(MPCWavHandle handle) {
    if (!handle) {
        return "";
    }

    WAV* wav = (WAV*)handle;
    return wav->title().c_str();
}

int mpc_wav_get_index(MPCWavHandle handle) {
    if (!handle) {
        return 0;
    }

    WAV* wav = (WAV*)handle;
    return wav->index();
}

// Upload audio to host
int mpc_upload_audio(const char* host_address,
                     uint32_t interface_number,
                     MPCWavHandle* wav_handles,
                     size_t wav_count,
                     MPCFormatHandle format,
                     bool loop_mode) {
    if (!host_address || !wav_handles || wav_count == 0 || !format) {
        return MPC_ERROR_INVALID_PARAM;
    }

    if (!g_lib_state.initialized) {
        mpc_init(nullptr);
    }

    // Parse the host address
    IPAddress connectAddress;
    if (!connectAddress.set_str(host_address)) {
        return MPC_ERROR_INVALID_PARAM;
    }
    connectAddress.set_ifno(interface_number);

    // Open TCP client socket
    TCPV6Client client;
    if (!client.open(true)) {
        return MPC_ERROR_SOCKET_OPEN;
    }

    // Connect to the host
    if (!client.connect(connectAddress)) {
        return MPC_ERROR_CONNECTION;
    }

    // Get the format
    FormatConfigure* wavFormat = (FormatConfigure*)format;
    FormatID formatId = *wavFormat;

    // Wait for acknowledgment of data transfer
    auto waitForAcknowledgment = [&](size_t transferCount) -> bool {
        while (true) {
            WAIT_CODE waitCode = client.wait(Clock::Seconds(2));
            if (waitCode == WAIT_CODE::ERROR) {
                return false;
            }
            if (waitCode == WAIT_CODE::WAKEUP) {
                MemoryPlayClient::ReceiveMessage receiveBuffer;
                client.receive(receiveBuffer);
                while (receiveBuffer.checkFrame()) {
                    if (receiveBuffer.getType() == 1) {
                        MemoryPlayClient::ReceiveMessageFrames frames(receiveBuffer.getFramePayload());
                        for (const auto& frame : frames) {
                            if (frame.first == "DataStack" || frame.first == "DataTag") {
                                if (transferCount == (size_t)atoll(frame.second.c_str()))
                                    return true;
                            }
                        }
                    }
                    receiveBuffer.next();
                }
                continue;
            }
            break;
        }
        return false;
    };

    // Send initial format and mute data
    MemoryPlayClient::SendMessageData sendData(false);
    size_t transferCount = 0;
    sendData.addData(BufferCS(reinterpret_cast<uint8_t*>(&formatId), sizeof(formatId)));
    client.send(sendData);
    sendData.clear();

    Buffer buffer;
    for (int i = 0; i < FILL_TIME; ++i) {
        buffer.resize(wavFormat->get1secSize());
        buffer.fill(wavFormat->getMuteByte());
        sendData.addData(BufferCS(reinterpret_cast<uint8_t*>(&formatId), sizeof(formatId)));
        sendData.addData(buffer);
        if (!client.send(sendData)) {
            return MPC_ERROR_CONNECTION;
        }
        ++transferCount;
        sendData.clear();
        if (!waitForAcknowledgment(transferCount)) {
            return MPC_ERROR_TIMEOUT;
        }
    }

    // Process WAV files in provided order
    WAV::ReadRest rest(*wavFormat);
    buffer.clear();

    for (size_t i = 0; i < wav_count; ++i) {
        WAV* wav = (WAV*)wav_handles[i];

        while (!wav->is_empty()) {
            Buffer tempBuffer;
            if (!wav->read(tempBuffer, wavFormat->get1secSize() - buffer.size(), rest)) {
                return MPC_ERROR_UNKNOWN;
            }
            if (tempBuffer.empty())
                break;

            buffer.insert(buffer.end(), tempBuffer.begin(), tempBuffer.end());
            if (buffer.size() >= wavFormat->get1secSize()) {
                sendData.addData(BufferCS(reinterpret_cast<uint8_t*>(&formatId), sizeof(formatId)));
                sendData.addData(buffer);
                if (!client.send(sendData)) {
                    return MPC_ERROR_CONNECTION;
                }
                ++transferCount;
                sendData.clear();
                if (!waitForAcknowledgment(transferCount)) {
                    return MPC_ERROR_TIMEOUT;
                }
                buffer.clear();
            }
        }

        if (!buffer.empty()) {
            sendData.addData(BufferCS(reinterpret_cast<uint8_t*>(&formatId), sizeof(formatId)));
            sendData.addData(buffer);
            if (!client.send(sendData)) {
                return MPC_ERROR_CONNECTION;
            }
            ++transferCount;
            sendData.clear();
            if (!waitForAcknowledgment(transferCount)) {
                return MPC_ERROR_TIMEOUT;
            }
            buffer.clear();
        }

        // Send tag
        MemoryPlayClient::SendMessageData tagData(true);
        tagData.addString(wav->title());
        if (!client.send(tagData)) {
            return MPC_ERROR_CONNECTION;
        }
        if (!waitForAcknowledgment(transferCount)) {
            return MPC_ERROR_TIMEOUT;
        }
    }

    // Send final rest data
    rest.final(buffer);
    if (!buffer.empty()) {
        sendData.addData(BufferCS(reinterpret_cast<uint8_t*>(&formatId), sizeof(formatId)));
        sendData.addData(buffer);
        if (!client.send(sendData)) {
            return MPC_ERROR_CONNECTION;
        }
        ++transferCount;
        sendData.clear();
        if (!waitForAcknowledgment(transferCount)) {
            return MPC_ERROR_TIMEOUT;
        }
        buffer.clear();
    }

    // Send loop tag if requested
    if (loop_mode) {
        MemoryPlayClient::SendMessageData tagData(true);
        tagData.addString("@@Diretta-TAG-LOOP@@");
        if (!client.send(tagData)) {
            return MPC_ERROR_CONNECTION;
        }
        if (!waitForAcknowledgment(transferCount)) {
            return MPC_ERROR_TIMEOUT;
        }

        for (int i = 0; i < FILL_TIME; ++i) {
            buffer.resize(wavFormat->get1secSize());
            buffer.fill(wavFormat->getMuteByte());
            sendData.addData(BufferCS(reinterpret_cast<uint8_t*>(&formatId), sizeof(formatId)));
            sendData.addData(buffer);
            if (!client.send(sendData)) {
                return MPC_ERROR_CONNECTION;
            }
            ++transferCount;
            sendData.clear();
            if (!waitForAcknowledgment(transferCount)) {
                return MPC_ERROR_TIMEOUT;
            }
        }
    }

    // Send quit tag
    {
        MemoryPlayClient::SendMessageData tagData(true);
        tagData.addString("@@Diretta-TAG-QUIT@@");
        if (!client.send(tagData)) {
            return MPC_ERROR_CONNECTION;
        }
        if (!waitForAcknowledgment(transferCount)) {
            return MPC_ERROR_TIMEOUT;
        }
    }

    for (int i = 0; i < FILL_TIME; ++i) {
        buffer.resize(wavFormat->get1secSize());
        buffer.fill(wavFormat->getMuteByte());
        sendData.addData(BufferCS(reinterpret_cast<uint8_t*>(&formatId), sizeof(formatId)));
        sendData.addData(buffer);
        if (!client.send(sendData)) {
            return MPC_ERROR_CONNECTION;
        }
        ++transferCount;
        sendData.clear();
        if (!waitForAcknowledgment(transferCount)) {
            return MPC_ERROR_TIMEOUT;
        }
    }

    return MPC_SUCCESS;
}

// Session-based control API
int mpc_session_create(const char* host_address,
                       uint32_t interface_number,
                       MPCSessionHandle* session) {
    if (!host_address || !session) {
        return MPC_ERROR_INVALID_PARAM;
    }

    if (!g_lib_state.initialized) {
        mpc_init(nullptr);
    }

    try {
        ControlSession* sess = new ControlSession();
        if (!sess->open(host_address, interface_number)) {
            delete sess;
            return MPC_ERROR_CONNECTION;
        }

        *session = (MPCSessionHandle)sess;
        return MPC_SUCCESS;
    } catch (...) {
        return MPC_ERROR_UNKNOWN;
    }
}

void mpc_session_close(MPCSessionHandle session) {
    if (session) {
        ControlSession* sess = (ControlSession*)session;
        sess->close();
        delete sess;
    }
}

int mpc_session_connect_target(MPCSessionHandle session,
                                const char* target_address,
                                uint32_t interface_number) {
    if (!session || !target_address) {
        return MPC_ERROR_INVALID_PARAM;
    }

    ControlSession* sess = (ControlSession*)session;
    return sess->connect_target(target_address, interface_number);
}

int mpc_session_play(MPCSessionHandle session) {
    if (!session) {
        return MPC_ERROR_INVALID_PARAM;
    }

    ControlSession* sess = (ControlSession*)session;
    return sess->play();
}

int mpc_session_pause(MPCSessionHandle session) {
    if (!session) {
        return MPC_ERROR_INVALID_PARAM;
    }

    ControlSession* sess = (ControlSession*)session;
    return sess->pause();
}

int mpc_session_seek(MPCSessionHandle session, int64_t offset_seconds) {
    if (!session) {
        return MPC_ERROR_INVALID_PARAM;
    }

    ControlSession* sess = (ControlSession*)session;
    return sess->seek(offset_seconds);
}

int mpc_session_seek_to_start(MPCSessionHandle session) {
    if (!session) {
        return MPC_ERROR_INVALID_PARAM;
    }

    ControlSession* sess = (ControlSession*)session;
    return sess->seek_to_start();
}

int mpc_session_seek_absolute(MPCSessionHandle session, int64_t position_seconds) {
    if (!session) {
        return MPC_ERROR_INVALID_PARAM;
    }

    ControlSession* sess = (ControlSession*)session;
    return sess->seek_absolute(position_seconds);
}

int mpc_session_quit(MPCSessionHandle session) {
    if (!session) {
        return MPC_ERROR_INVALID_PARAM;
    }

    ControlSession* sess = (ControlSession*)session;
    return sess->quit();
}

int mpc_session_get_play_status(MPCSessionHandle session, MPCPlaybackStatus* status) {
    if (!session || !status) {
        return MPC_ERROR_INVALID_PARAM;
    }

    ControlSession* sess = (ControlSession*)session;
    return sess->get_play_status(status);
}

int mpc_session_get_current_time(MPCSessionHandle session, int64_t* time_seconds) {
    if (!session || !time_seconds) {
        return MPC_ERROR_INVALID_PARAM;
    }

    ControlSession* sess = (ControlSession*)session;
    return sess->get_current_time(time_seconds);
}

int mpc_session_get_tag_list(MPCSessionHandle session, MPCTagList** tag_list) {
    if (!session || !tag_list) {
        return MPC_ERROR_INVALID_PARAM;
    }

    ControlSession* sess = (ControlSession*)session;
    vector<string> tags;

    int result = sess->get_tag_list(tags);
    if (result != MPC_SUCCESS) {
        return result;
    }

    // Allocate the tag list structure
    MPCTagList* list = (MPCTagList*)malloc(sizeof(MPCTagList));
    if (!list) {
        return MPC_ERROR_MEMORY;
    }

    list->count = tags.size();
    if (list->count > 0) {
        list->tags = (MPCTagInfo*)calloc(list->count, sizeof(MPCTagInfo));
        if (!list->tags) {
            free(list);
            return MPC_ERROR_MEMORY;
        }

        for (size_t i = 0; i < list->count; i++) {
            strncpy(list->tags[i].tag, tags[i].c_str(), sizeof(list->tags[i].tag) - 1);
            list->tags[i].tag[sizeof(list->tags[i].tag) - 1] = '\0';
        }
    } else {
        list->tags = nullptr;
    }

    *tag_list = list;
    return MPC_SUCCESS;
}

void mpc_free_tag_list(MPCTagList* tag_list) {
    if (tag_list) {
        if (tag_list->tags) {
            free(tag_list->tags);
        }
        free(tag_list);
    }
}

// Get error message string
const char* mpc_error_string(int error_code) {
    switch (error_code) {
        case MPC_SUCCESS:
            return "Success";
        case MPC_ERROR_SOCKET_OPEN:
            return "Failed to open socket";
        case MPC_ERROR_FIND_TARGET:
            return "Failed to find targets";
        case MPC_ERROR_NO_HOSTS_FOUND:
            return "No MemoryPlayHost instances found";
        case MPC_ERROR_INVALID_PARAM:
            return "Invalid parameter";
        case MPC_ERROR_CONNECTION:
            return "Connection error";
        case MPC_ERROR_TIMEOUT:
            return "Operation timed out";
        case MPC_ERROR_MEMORY:
            return "Memory allocation failed";
        case MPC_ERROR_UNKNOWN:
            return "Unknown error";
        default:
            return "Unrecognized error code";
    }
}
