#include "lib_memory_play_controller.h"

#include <iostream>
#include <string>
#include <cstring>
#include <vector>
#include <sstream>
#include <map>
#include <filesystem>

#include <Diretta/Find>
#include <Diretta/SysLog>
#include <ACQUA/TCPV6>
#if __has_include("../MemoryPlayHost/MemoryPlayControll.hpp")
	#include "../MemoryPlayHost/MemoryPlayControll.hpp"
#else
	#include "MemoryPlayControll.hpp"
#endif

#include "WAV.hpp"

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
    Clock lastrecv = Clock::now();

    while (true) {
//        cout << "starting client wait " << lastrecv.getMilliSeconds() << endl;
        WAIT_CODE wi = client.wait(Clock::MilliSeconds(100));

        if (wi == WAIT_CODE::ERROR) {
            SysLog::Error << "Socket Error";
            return MPC_ERROR_CONNECTION;
        }

        if (wi == WAIT_CODE::TIMEOUT) {
            if (Clock::now() - lastrecv >= Clock::MilliSeconds(timeoutMs)) {
//                cout << "break" << endl;
                return MPC_ERROR_TIMEOUT;
            }
//            cout << "continue" << endl;
            continue;
        }

        if (wi == WAIT_CODE::WAKEUP) {
            MemoryPlayControll::ReceiveMessage rcvBuf;
            if (!client.receive(rcvBuf)) {
                SysLog::Error << "Socket Error";
                return MPC_ERROR_CONNECTION;
            }

            while (rcvBuf.checkFrame()) {
//                SysLog::Info << "FrameType : " << rcvBuf.getType();

                if (rcvBuf.getType() == 1) {
                    MemoryPlayControll::ReceiveMessageFrames frames(rcvBuf.getFramePayload());
                    for (const auto& i : frames) {
                        SysLog::Debug << "GetMessage " << i.first << "=" << i.second;
//                        cout << "GetMessage " << i.first << "=" << i.second << endl;
                        lastrecv = Clock::now();
                        if (handleFrame(i.first, i.second)) {
                            return MPC_SUCCESS;
                        }
                    }
                }

                rcvBuf.next();
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
        if (!connectadd.set_str(host_address)) {
            SysLog::Error << "Invalid host address";
            return false;
        }
        connectadd.set_ifno(interface_number);

        if (!client.open(true)) {
            SysLog::Error << "Socket Open Error";
            return false;
        }

        if (!client.connect(connectadd)) {
            SysLog::Error << "Host Connect Error " << client.Errcode;
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

        MemoryPlayControll::SnedMessageFrames header;
        stringstream ss;
        ss << target_address << " " << interface_number;
        header.addHeader("Connect", ss.str());
//        cout << "Connecting to: " << ss.str() << endl;

        if (!client.send(header)) {
            SysLog::Error << "Send Error";
            return MPC_ERROR_CONNECTION;
        }

        return MPC_SUCCESS;
    }

    int play() {
        if (!connected) {
            return MPC_ERROR_CONNECTION;
        }

        MemoryPlayControll::SnedMessageFrames header;
        header.addHeader("Play", "");

        if (!client.send(header)) {
            SysLog::Error << "Send Error";
            return MPC_ERROR_CONNECTION;
        }

        return MPC_SUCCESS;
    }

    int pause() {
        if (!connected) {
            return MPC_ERROR_CONNECTION;
        }

        MemoryPlayControll::SnedMessageFrames header;
        header.addHeader("Pause", "");

        if (!client.send(header)) {
            SysLog::Error << "Send Error";
            return MPC_ERROR_CONNECTION;
        }

        return MPC_SUCCESS;
    }

    int seek(int64_t offset_seconds) {
        if (!connected) {
            return MPC_ERROR_CONNECTION;
        }

        MemoryPlayControll::SnedMessageFrames header;
        stringstream ss;
        if (offset_seconds > 0) {
            ss << "+" << offset_seconds;
        } else {
            ss << offset_seconds;
        }
        header.addHeader("Seek", ss.str());

        if (!client.send(header)) {
            SysLog::Error << "Send Error";
            return MPC_ERROR_CONNECTION;
        }

        return MPC_SUCCESS;
    }

    int seek_to_start() {
        if (!connected) {
            return MPC_ERROR_CONNECTION;
        }

        MemoryPlayControll::SnedMessageFrames header;
        header.addHeader("Seek", "Front");

        if (!client.send(header)) {
            SysLog::Error << "Send Error";
            return MPC_ERROR_CONNECTION;
        }

        return MPC_SUCCESS;
    }

    int seek_absolute(int64_t position_seconds) {
        if (!connected) {
            return MPC_ERROR_CONNECTION;
        }

        MemoryPlayControll::SnedMessageFrames header;
        stringstream ss;
        ss << position_seconds;  // No + or - prefix, just the absolute position
        header.addHeader("Seek", ss.str());

        if (!client.send(header)) {
            SysLog::Error << "Send Error";
            return MPC_ERROR_CONNECTION;
        }

        return MPC_SUCCESS;
    }

    int quit() {
        if (!connected) {
            return MPC_ERROR_CONNECTION;
        }

        MemoryPlayControll::SnedMessageFrames header;
        header.addHeader("Seek", "Quit");

        if (!client.send(header)) {
            SysLog::Error << "Send Error";
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
        MemoryPlayControll::SnedMessageFrames header;
        header.addHeader("Request", "Status");
        if (!client.send(header)) {
            SysLog::Error << "Send Error";
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
        MemoryPlayControll::SnedMessageFrames header;
        header.addHeader("Request", "Status");
        if (!client.send(header)) {
            SysLog::Error << "Send Error";
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
        MemoryPlayControll::SnedMessageFrames header;
        header.addHeader("Request", "Status");
        if (!client.send(header)) {
            SysLog::Error << "Send Error";
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
    IPAddress connectadd;
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

    // Initialize Diretta logging system
    if (g_lib_state.logging_enabled) {
        SysLogDiretta::initialize(ACQUA::SysLog::local0, DIRETTA::SyslogPortHost, g_lib_state.logging_enabled);

        if (g_lib_state.verbose_mode) {
            SysLogDiretta::changeLevel(SysLog::Debug, DIRETTA::SyslogPortHost);
        } else {
            SysLogDiretta::changeLevel(SysLog::Info, DIRETTA::SyslogPortHost);
        }
    } else {
        SysLogDiretta::initialize(ACQUA::SysLog::local0, DIRETTA::SyslogPortHost, false);
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

    // Set up Find configuration (from lines 365-368 of original code)
    Find::Setting fs;
    fs.Name = "MemoryPlayController";
    fs.ProductID = 0;
    fs.Loopback = true;

    // Create and open the Find socket (lines 370-374)
    Find find(fs);
    if (!find.open()) {
        SysLog::Error << "Socket Open Error";
        return MPC_ERROR_SOCKET_OPEN;
    }

    // Find targets (lines 375-384)
    Find::TargetResalts res;
    Find::PortResalts pres;

    if (!find.findTarget(res)) {
        SysLog::Error << "findTarget1 Error";
        return MPC_ERROR_FIND_TARGET;
    }

    if (!find.findTarget(res, pres, Find::AUDIO_MEMORY)) {
        SysLog::Error << "findTarget2 Error";
        return MPC_ERROR_FIND_TARGET;
    }

    if (pres.empty()) {
        SysLog::Error << "Can not found MemoryPlayHost";
        return MPC_ERROR_NO_HOSTS_FOUND;
    }

    // Allocate the host list structure
    MPCHostList* list = (MPCHostList*)malloc(sizeof(MPCHostList));
    if (!list) {
        return MPC_ERROR_MEMORY;
    }

    list->count = pres.size();
    list->hosts = (MPCHostInfo*)calloc(list->count, sizeof(MPCHostInfo));
    if (!list->hosts) {
        free(list);
        return MPC_ERROR_MEMORY;
    }

    // Populate the host list (adapted from lines 396-403)
    size_t idx = 0;
    for (const auto& r : pres) {
        MPCHostInfo* host = &list->hosts[idx];

        // Get IP address string
        string addr_str;
        r.first.get_str(addr_str);
        strncpy(host->ip_address, addr_str.c_str(), sizeof(host->ip_address) - 1);
        host->ip_address[sizeof(host->ip_address) - 1] = '\0';

        // Get interface number
        host->interface_number = r.first.get_ifno();

        // Get target and output names
        strncpy(host->target_name, r.second.targetName.c_str(), sizeof(host->target_name) - 1);
        host->target_name[sizeof(host->target_name) - 1] = '\0';

        strncpy(host->output_name, r.second.outputName.c_str(), sizeof(host->output_name) - 1);
        host->output_name[sizeof(host->output_name) - 1] = '\0';

        // Check if loopback
        host->is_loopback = r.first.is_loopback();

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
    IPAddress connectadd;
    if (!connectadd.set_str(host_address)) {
        SysLog::Error << "Invalid host address";
        return MPC_ERROR_INVALID_PARAM;
    }
    connectadd.set_ifno(interface_number);

    // Open TCP client socket (from lines 429-434)
    TCPV6Client client;
    if (!client.open(true)) {
        SysLog::Error << "Socket Open Error";
        return MPC_ERROR_SOCKET_OPEN;
    }

    // Connect to the host (from lines 438-441)
    if (!client.connect(connectadd)) {
        SysLog::Error << "Host Connect Error " << client.Errcode;
        return MPC_ERROR_CONNECTION;
    }

    SysLog::Notice << "Host Connect";

    // Send target list request (from lines 664-668)
    MemoryPlayControll::SnedMessageFrames header;
    header.addHeader("Request", "TargetList");
    if (!client.send(header)) {
        SysLog::Error << "Send Error";
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
            SysLog::Error << "Failed to open audio file: " << filename;
            return MPC_ERROR_INVALID_PARAM;
        }

        *handle = (MPCWavHandle)wav;
        return MPC_SUCCESS;
    } catch (...) {
        SysLog::Error << "Exception opening WAV file";
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
        FormatConfigure* fmt = new FormatConfigure(wav->getFmt());
        *format = (MPCFormatHandle)fmt;
        return MPC_SUCCESS;
    } catch (...) {
        return MPC_ERROR_UNKNOWN;
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
    IPAddress connectadd;
    if (!connectadd.set_str(host_address)) {
        SysLog::Error << "Invalid host address";
        return MPC_ERROR_INVALID_PARAM;
    }
    connectadd.set_ifno(interface_number);

    // Open TCP client socket
    TCPV6Client client;
    if (!client.open(true)) {
        SysLog::Error << "Socket Open Error";
        return MPC_ERROR_SOCKET_OPEN;
    }

    // Connect to the host
    if (!client.connect(connectadd)) {
        SysLog::Error << "Host Connect Error " << client.Errcode;
        return MPC_ERROR_CONNECTION;
    }

    // Get the format
    FormatConfigure* wavformat = (FormatConfigure*)format;
    FormatID indef = *wavformat;

    // Response check lambda
    auto rev_check = [&](size_t tcount) -> bool {
        while (true) {
            WAIT_CODE wi = client.wait(Clock::Seconds(2));
            if (wi == WAIT_CODE::ERROR) {
                return false;
            }
            if (wi == WAIT_CODE::WAKEUP) {
                MemoryPlayControll::ReceiveMessage rcvBuf;
                client.receive(rcvBuf);
                while (rcvBuf.checkFrame()) {
                    if (rcvBuf.getType() == 1) {
                        MemoryPlayControll::ReceiveMessageFrames frames(rcvBuf.getFramePayload());
                        for (const auto& i : frames) {
                            if (i.first == "DataStack" || i.first == "DataTag") {
                                if (tcount == (size_t)atoll(i.second.c_str()))
                                    return true;
                            }
                        }
                    }
                    rcvBuf.next();
                }
                continue;
            }
            break;
        }
        SysLog::Error << "Timeout Data";
        return false;
    };

    // Send initial format and mute data
    MemoryPlayControll::SnedMessageData sdata(false);
    size_t tCount = 0;
    sdata.addData(BufferCS(reinterpret_cast<uint8_t*>(&indef), sizeof(indef)));
    client.send(sdata);
    sdata.clear();

    Buffer buf;
    for (int a = 0; a < FILL_TIME; ++a) {
        buf.resize(wavformat->get1secSize());
        buf.fill(wavformat->getMuteByte());
        sdata.addData(BufferCS(reinterpret_cast<uint8_t*>(&indef), sizeof(indef)));
        sdata.addData(buf);
        if (!client.send(sdata)) {
            SysLog::Error << "Socket Error";
            return MPC_ERROR_CONNECTION;
        }
        ++tCount;
        sdata.clear();
        if (!rev_check(tCount)) {
            return MPC_ERROR_TIMEOUT;
        }
    }

    // Process WAV files in provided order
    WAV::ReadRese rest(*wavformat);
    buf.clear();

    for (size_t i = 0; i < wav_count; ++i) {
        WAV* wav = (WAV*)wav_handles[i];

        while (!wav->is_empty()) {
            Buffer tmp;
            wav->read(tmp, wavformat->get1secSize() - buf.size(), rest);
            if (tmp.empty())
                break;

            buf.insert(buf.end(), tmp.begin(), tmp.end());
            if (buf.size() >= wavformat->get1secSize()) {
                sdata.addData(BufferCS(reinterpret_cast<uint8_t*>(&indef), sizeof(indef)));
                sdata.addData(buf);
                if (!client.send(sdata)) {
                    SysLog::Error << "Socket Error";
                    return MPC_ERROR_CONNECTION;
                }
                ++tCount;
                sdata.clear();
                if (!rev_check(tCount)) {
                    return MPC_ERROR_TIMEOUT;
                }
                buf.clear();
            }
        }

        if (!buf.empty()) {
            sdata.addData(BufferCS(reinterpret_cast<uint8_t*>(&indef), sizeof(indef)));
            sdata.addData(buf);
            if (!client.send(sdata)) {
                SysLog::Error << "Socket Error";
                return MPC_ERROR_CONNECTION;
            }
            ++tCount;
            sdata.clear();
            if (!rev_check(tCount)) {
                return MPC_ERROR_TIMEOUT;
            }
            buf.clear();
        }

        // Send tag
        MemoryPlayControll::SnedMessageData tdata(true);
        tdata.addString(wav->title());
        if (!client.send(tdata)) {
            SysLog::Error << "Socket Error";
            return MPC_ERROR_CONNECTION;
        }
        if (!rev_check(tCount)) {
            return MPC_ERROR_TIMEOUT;
        }
    }

    // Send final rest data
    rest.final(buf);
    if (!buf.empty()) {
        sdata.addData(BufferCS(reinterpret_cast<uint8_t*>(&indef), sizeof(indef)));
        sdata.addData(buf);
        if (!client.send(sdata)) {
            SysLog::Error << "Socket Error";
            return MPC_ERROR_CONNECTION;
        }
        ++tCount;
        sdata.clear();
        if (!rev_check(tCount)) {
            return MPC_ERROR_TIMEOUT;
        }
        buf.clear();
    }

    // Send loop tag if requested
    if (loop_mode) {
        MemoryPlayControll::SnedMessageData tdata(true);
        tdata.addString("@@Diretta-TAG-LOOP@@");
        if (!client.send(tdata)) {
            SysLog::Error << "Socket Error";
            return MPC_ERROR_CONNECTION;
        }
        if (!rev_check(tCount)) {
            return MPC_ERROR_TIMEOUT;
        }

        for (int a = 0; a < FILL_TIME; ++a) {
            buf.resize(wavformat->get1secSize());
            buf.fill(wavformat->getMuteByte());
            sdata.addData(BufferCS(reinterpret_cast<uint8_t*>(&indef), sizeof(indef)));
            sdata.addData(buf);
            if (!client.send(sdata)) {
                SysLog::Error << "Socket Error";
                return MPC_ERROR_CONNECTION;
            }
            ++tCount;
            sdata.clear();
            if (!rev_check(tCount)) {
                return MPC_ERROR_TIMEOUT;
            }
        }
    }

    // Send quit tag
    {
        MemoryPlayControll::SnedMessageData tdata(true);
        tdata.addString("@@Diretta-TAG-QUIT@@");
        if (!client.send(tdata)) {
            SysLog::Error << "Socket Error";
            return MPC_ERROR_CONNECTION;
        }
        if (!rev_check(tCount)) {
            return MPC_ERROR_TIMEOUT;
        }
    }

    for (int a = 0; a < FILL_TIME; ++a) {
        buf.resize(wavformat->get1secSize());
        buf.fill(wavformat->getMuteByte());
        sdata.addData(BufferCS(reinterpret_cast<uint8_t*>(&indef), sizeof(indef)));
        sdata.addData(buf);
        if (!client.send(sdata)) {
            SysLog::Error << "Socket Error";
            return MPC_ERROR_CONNECTION;
        }
        ++tCount;
        sdata.clear();
        if (!rev_check(tCount)) {
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
        SysLog::Error << "Exception creating session";
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
        default:
            return "Unknown error";
    }
}
