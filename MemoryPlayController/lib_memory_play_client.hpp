#pragma once

#include <cstdint>
#include <cstring>
#include <string>
#include <list>
#include <sstream>
#include <ACQUA/Buffer>

namespace MemoryPlayClient {

    // Frame utilities for reading/writing multi-byte values
    struct Frame {
        // 1-byte operations
        static std::uint8_t read1Byte(const std::uint8_t* d) {
            return std::uint8_t(d[0]);
        }

        static void write1Byte(std::uint8_t* d, std::uint8_t v) {
            d[0] = v & 0xFF;
        }

        // 2-byte operations (big-endian)
        static std::uint16_t read2Byte(const std::uint8_t* d) {
            return (std::uint16_t(d[0]) << 8) | (std::uint16_t(d[1]) << 0);
        }

        static void write2Byte(std::uint8_t* d, std::uint16_t v) {
            d[0] = (v >> 8) & 0xFF;
            d[1] = (v >> 0) & 0xFF;
        }

        // 3-byte operations (big-endian)
        static std::uint32_t read3Byte(const std::uint8_t* d) {
            return (std::uint32_t(d[0]) << 16) | (std::uint32_t(d[1]) << 8) | (std::uint32_t(d[2]) << 0);
        }

        static void write3Byte(std::uint8_t* d, std::uint32_t v) {
            d[0] = (v >> 16) & 0xFF;
            d[1] = (v >> 8) & 0xFF;
            d[2] = (v >> 0) & 0xFF;
        }

        // 4-byte operations (big-endian)
        static std::uint32_t read4Byte(const std::uint8_t* d) {
            return (std::uint32_t(d[0]) << 24) | (std::uint32_t(d[1]) << 16) |
                   (std::uint32_t(d[2]) << 8) | (std::uint32_t(d[3]) << 0);
        }

        static void write4Byte(std::uint8_t* d, std::uint32_t v) {
            d[0] = (v >> 24) & 0xFF;
            d[1] = (v >> 16) & 0xFF;
            d[2] = (v >> 8) & 0xFF;
            d[3] = (v >> 0) & 0xFF;
        }
    };

    // Message type enumeration
    enum class SendMessageType : uint8_t {
        Data = 0,
        Command = 1,
        Tag = 2
    };

    // Payload header structure
    struct PayloadHeader : Frame {
        std::uint8_t data[9];

        void setLength(std::size_t s) {
            write3Byte(data + 0, std::uint32_t(s));
        }

        std::size_t getLength() const {
            return std::size_t(read3Byte(data + 0));
        }

        void setType(SendMessageType t) {
            write1Byte(data + 3, std::uint8_t(t));
        }

        std::uint8_t getType() const {
            return read1Byte(data + 3);
        }

        void setFlags(std::uint8_t t) {
            write1Byte(data + 4, t);
        }

        std::uint8_t getFlags() const {
            return read1Byte(data + 4);
        }

        void setIdentifier(std::uint32_t i) {
            write4Byte(data + 5, i);
        }

        std::uint32_t getIdentifier() const {
            return read4Byte(data + 5);
        }
    };

    // Data header structure
    struct DataHeader : Frame {
        std::uint8_t data[1];

        void setPad(std::uint8_t p) {
            write1Byte(data + 0, p);
        }

        std::uint8_t getPad() const {
            return read1Byte(data + 0);
        }
    };

    // Headers header structure
    struct HeadersHeader : Frame {
        std::uint8_t data[6];

        void setPad(std::uint8_t p) {
            write1Byte(data + 0, p);
        }

        std::uint8_t getPad() const {
            return read1Byte(data + 0);
        }

        void setDependency(std::uint32_t d) {
            write4Byte(data + 1, d);
        }

        std::uint32_t getDependency() const {
            return read4Byte(data + 1);
        }

        void setWeight(std::uint8_t p) {
            write1Byte(data + 5, p);
        }

        std::uint8_t getWeight() const {
            return read1Byte(data + 5);
        }
    };

    // Base message class for sending
    class SendMessage : public ACQUA::Buffer {
    public:
        explicit SendMessage(size_t ps) : baseSize(ps) {
            resize(0);
            payloadHeader().setType(SendMessageType(0));
            payloadHeader().setFlags(0);
            payloadHeader().setIdentifier(0);
        }

        PayloadHeader& payloadHeader() {
            return *reinterpret_cast<PayloadHeader*>(data());
        }

        const PayloadHeader& payloadHeader() const {
            return *reinterpret_cast<const PayloadHeader*>(data());
        }

        std::uint8_t* frameHeader() {
            return data() + sizeof(PayloadHeader);
        }

        const std::uint8_t* frameHeader() const {
            return data() + sizeof(PayloadHeader);
        }

        std::uint8_t* payload() {
            return data() + sizeof(PayloadHeader) + baseSize;
        }

        const std::uint8_t* payload() const {
            return data() + sizeof(PayloadHeader) + baseSize;
        }

        void resize(size_t s) {
            ACQUA::Buffer::resize(sizeof(PayloadHeader) + baseSize + s);
            payloadHeader().setLength(s + baseSize);
        }

        size_t size() const {
            return ACQUA::Buffer::size() - sizeof(PayloadHeader) - baseSize;
        }

        std::uint8_t* increaseSize(size_t p) {
            size_t ns = size();
            resize(ns + p);
            return data() + sizeof(PayloadHeader) + baseSize + ns;
        }

        void clear() {
            resize(0);
        }

        const size_t baseSize;
    };

    // Message class for sending data/tag payloads
    class SendMessageData : public SendMessage {
    public:
        explicit SendMessageData(bool tag) : SendMessage(sizeof(DataHeader)) {
            payloadHeader().setType(tag ? SendMessageType::Tag : SendMessageType::Data);
            dataHeader().setPad(0);
        }

        DataHeader& dataHeader() {
            return *reinterpret_cast<DataHeader*>(SendMessage::frameHeader());
        }

        const DataHeader& dataHeader() const {
            return *reinterpret_cast<const DataHeader*>(SendMessage::frameHeader());
        }

        void addData(ACQUA::BufferCS_const data) {
            uint8_t* m = increaseSize(data.size());
            std::memcpy(m, data.get(), data.size());
        }

        void addString(const std::string& str) {
            uint8_t* m = increaseSize(str.size());
            std::memcpy(m, str.c_str(), str.size());
        }
    };

    // Message class for sending command frames with headers
    class SendMessageFrames : public SendMessage {
    public:
        SendMessageFrames() : SendMessage(sizeof(HeadersHeader)) {
            payloadHeader().setType(SendMessageType::Command);
            headersHeader().setDependency(0);
            headersHeader().setPad(0);
            headersHeader().setWeight(0);
        }

        HeadersHeader& headersHeader() {
            return *reinterpret_cast<HeadersHeader*>(SendMessage::frameHeader());
        }

        const HeadersHeader& headersHeader() const {
            return *reinterpret_cast<const HeadersHeader*>(SendMessage::frameHeader());
        }

        void addHeader(const std::string& k, std::int64_t v) {
            std::stringstream ss;
            ss << v;
            addHeader(k, ss.str());
        }

        void addHeader(const std::string& k, const std::string& v) {
            uint8_t* m = increaseSize(k.size() + v.size() + 3);
            std::memcpy(m, &k.front(), k.size());
            m += k.size();
            m[0] = '=';
            ++m;
            if (!v.empty()) {
                std::memcpy(m, &v.front(), v.size());
                m += v.size();
            }
            m[0] = '\r';
            ++m;
            m[0] = '\n';
        }
    };

    // Message class for receiving messages
    class ReceiveMessage : public ACQUA::Buffer {
    public:
        ReceiveMessage() : type(0), frameLength(0) {}

        bool checkFrame() {
            if (size() < sizeof(PayloadHeader))
                return false;

            const PayloadHeader& frameHeader = *reinterpret_cast<const PayloadHeader*>(data());
            frameLength = frameHeader.getLength();

            if (size() < frameLength + sizeof(PayloadHeader))
                return false;

            type = frameHeader.getType();

            switch (type) {
                case 0: // data
                case 2: // tag
                    return true;
                case 1: // headers
                    if (size() < sizeof(PayloadHeader) + sizeof(HeadersHeader))
                        return false;
                    return true;
                default:
                    return false;
            }
        }

        void next() {
            erase(begin(), begin() + frameLength + sizeof(PayloadHeader));
        }

        std::uint16_t getType() const {
            return type;
        }

        ACQUA::BufferCS_const getFramePayload() const {
            return ACQUA::BufferCS_const(data() + sizeof(PayloadHeader), frameLength);
        }

    private:
        std::uint8_t type;
        std::size_t frameLength;
    };

    // Received data message wrapper
    class ReceiveMessageData : public ACQUA::BufferCS_const {
    public:
        explicit ReceiveMessageData(ACQUA::BufferCS_const data)
            : ACQUA::BufferCS_const(data.get() + sizeof(DataHeader),
                                     data.size() - sizeof(DataHeader)) {
        }
    };

    // Received frames message - parses key=value pairs
    class ReceiveMessageFrames : public std::list<std::pair<std::string, std::string>> {
    public:
        explicit ReceiveMessageFrames(ACQUA::BufferCS_const data) {
            bool state = false;
            std::string key, value;

            for (const char* c = data.get_char() + sizeof(HeadersHeader);
                 c < data.get_char() + data.size();
                 ++c) {
                if (*c == '\r' || *c == '\n') {
                    state = false;
                    if (!key.empty()) {
                        push_back(std::make_pair(key, value));
                    }
                    key.clear();
                    value.clear();
                    continue;
                }

                if (!state) { // parsing key
                    if (*c == '=') {
                        state = true;
                    } else {
                        key += *c;
                    }
                } else { // parsing value
                    value += *c;
                }
            }

            if (!key.empty()) {
                push_back(std::make_pair(key, value));
            }
        }
    };

} // namespace MemoryPlayClient