#pragma once

#include <cstdint>
#include <string>
#include <fstream>
#include <filesystem>
#include <functional>
#include <array>

#include <Diretta/Format>
#include <ACQUA/Buffer>

class WAV {
public:
    // Audio format modes supported by the library
    enum class FormatMode : int {
        None = 0,
        PCM = 1,
        DSF = 2,
        DFF = 3,
        AIFF = 5
    };

    // Helper class for DSD bit stream reassembly (MSB/LSB ordering)
    class ReadRest {
        friend class WAV;
    public:
        explicit ReadRest(DIRETTA::FormatConfigure& format);
        void final(ACQUA::Buffer& buffer);

    private:
        void push8_msb(const std::uint8_t* bytes, int bits = 8);
        void push8_lsb(const std::uint8_t* bytes, int bits = 8);
        bool full(std::uint32_t* output);
        void push8(const std::uint8_t* bytes);
        void push8(const std::uint8_t* bytes, int bits);

        static constexpr std::size_t MAX_CHANNELS = 32;
        static const std::uint8_t SWAP_BITS_TABLE[256];

        const DIRETTA::FormatConfigure format_;
        const int channel_count_;
        std::uint64_t rest_[MAX_CHANNELS];
        int bit_count_;
    };

    WAV() = default;
    ~WAV() { close(); }

    // Disable copy, allow move
    WAV(const WAV&) = delete;
    WAV& operator=(const WAV&) = delete;
    WAV(WAV&&) = default;
    WAV& operator=(WAV&&) = default;

    // File operations
    [[nodiscard]] bool open(const std::filesystem::path& filename, bool convert_to_2ch_32bit);
    void close();
    [[nodiscard]] bool is_open() const { return file_.is_open(); }
    [[nodiscard]] bool is_empty() const { return end_of_stream_ || file_.fail(); }

    // Data reading
    [[nodiscard]] bool read(ACQUA::Buffer& buffer, std::size_t target_bytes, ReadRest& rest);

    // Format information
    [[nodiscard]] DIRETTA::FormatConfigure getFormat() const {
        return convert_to_2ch_32bit_ ? format_2ch_32bit_ : format_;
    }
    [[nodiscard]] const std::string& title() const { return title_; }
    [[nodiscard]] int index() const { return track_index_; }

private:
    // Format-specific read methods
    [[nodiscard]] bool readPCM(ACQUA::Buffer& buffer, std::size_t target_bytes);
    [[nodiscard]] bool readDSF(ACQUA::Buffer& buffer, std::size_t target_bytes, ReadRest& rest);
    [[nodiscard]] bool readDFF(ACQUA::Buffer& buffer, std::size_t target_bytes, ReadRest& rest);
    [[nodiscard]] bool readAIFF(ACQUA::Buffer& buffer, std::size_t target_bytes);

    // DFF helper methods
    [[nodiscard]] bool processDFF(std::function<bool(std::ifstream&, std::size_t&)> read_func);
    [[nodiscard]] bool readDFFChunk(std::function<bool(std::ifstream&, std::size_t&)> read_func);

    // M4A/ALAC helper
    int64_t readChildM4A(int64_t remaining_size);

    // Binary read helpers (little-endian)
    std::uint64_t read8bytes();
    std::uint32_t read4bytes();
    std::uint16_t read2bytes();
    std::uint8_t read1byte();

    // Binary read helpers (big-endian)
    std::uint64_t read8bytesBIG();
    std::uint32_t read4bytesBIG();
    std::uint16_t read2bytesBIG();

    // File state
    std::ifstream file_;
    std::filesystem::path file_path_;

    // Format configuration
    DIRETTA::FormatConfigure format_;
    DIRETTA::FormatConfigure format_2ch_32bit_;
    FormatMode mode_ = FormatMode::None;
    bool convert_to_2ch_32bit_ = false;
    bool end_of_stream_ = false;

    // Metadata
    std::string title_;
    int track_index_ = 0;

    // PCM state
    std::uint32_t pcm_data_remaining_ = 0;

    // DSD/DSF state
    std::uint64_t dsd_data_remaining_ = 0;
    std::uint64_t dsd_samples_remaining_ = 0;
    int dsd_block_size_ = 0;
    int dsd_channel_count_ = 0;
    ACQUA::Buffer dsd_buffer_;
    std::size_t dsd_buffer_remaining_ = 0;

    // DFF state
    struct DFFState {
        std::uint64_t chunk_size = 0;
        std::uint32_t type = 0;
        std::uint64_t current_size = 0;
        std::uint64_t read_reset = 0;
    } dff_state_;
};