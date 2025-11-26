#include "lib_wav.hpp"
#include <iostream>
#include <cstring>
#include <array>

using namespace std;
using namespace ACQUA;
using namespace DIRETTA;

namespace {
    // UTF-16 to UTF-8 conversion helpers
    bool ConvChU32ToU8(const char32_t u32Ch, std::array<char, 4>& u8Ch) {
        if (u32Ch < 0 || u32Ch > 0x10FFFF) {
            return false;
        }

        if (u32Ch < 128) {
            u8Ch[0] = char(u32Ch);
            u8Ch[1] = 0;
            u8Ch[2] = 0;
            u8Ch[3] = 0;
        } else if (u32Ch < 2048) {
            u8Ch[0] = 0xC0 | char(u32Ch >> 6);
            u8Ch[1] = 0x80 | (char(u32Ch) & 0x3F);
            u8Ch[2] = 0;
            u8Ch[3] = 0;
        } else if (u32Ch < 65536) {
            u8Ch[0] = 0xE0 | char(u32Ch >> 12);
            u8Ch[1] = 0x80 | (char(u32Ch >> 6) & 0x3F);
            u8Ch[2] = 0x80 | (char(u32Ch) & 0x3F);
            u8Ch[3] = 0;
        } else {
            u8Ch[0] = 0xF0 | char(u32Ch >> 18);
            u8Ch[1] = 0x80 | (char(u32Ch >> 12) & 0x3F);
            u8Ch[2] = 0x80 | (char(u32Ch >> 6) & 0x3F);
            u8Ch[3] = 0x80 | (char(u32Ch) & 0x3F);
        }

        return true;
    }

    bool IsU16HighSurrogate(char16_t ch) { return 0xD800 <= ch && ch < 0xDC00; }
    bool IsU16LowSurrogate(char16_t ch) { return 0xDC00 <= ch && ch < 0xE000; }

    bool ConvChU16ToU32(const std::array<char16_t, 2>& u16Ch, char32_t& u32Ch) {
        if (IsU16HighSurrogate(u16Ch[0])) {
            if (IsU16LowSurrogate(u16Ch[1])) {
                u32Ch = 0x10000 + (char32_t(u16Ch[0]) - 0xD800) * 0x400 +
                        (char32_t(u16Ch[1]) - 0xDC00);
            } else if (u16Ch[1] == 0) {
                u32Ch = u16Ch[0];
            } else {
                return false;
            }
        } else if (IsU16LowSurrogate(u16Ch[0])) {
            if (u16Ch[1] == 0) {
                u32Ch = u16Ch[0];
            } else {
                return false;
            }
        } else {
            u32Ch = u16Ch[0];
        }

        return true;
    }

    bool ConvChU16ToU8(const std::array<char16_t, 2>& u16Ch, std::array<char, 4>& u8Ch) {
        char32_t u32Ch;
        if (!ConvChU16ToU32(u16Ch, u32Ch)) {
            return false;
        }
        if (!ConvChU32ToU8(u32Ch, u8Ch)) {
            return false;
        }
        return true;
    }

    u8string utf16utf8(const u16string& str) {
        u8string ret;
        for (auto c : str) {
            std::array<char16_t, 2> u16Ch;
            u16Ch[0] = c;
            u16Ch[1] = 0;  // Fixed typo: was u16Ch[1]-0
            std::array<char, 4> u8Ch;
            ConvChU16ToU8(u16Ch, u8Ch);
            if (u8Ch[0] != 0)
                ret.push_back(u8Ch[0]);
            if (u8Ch[1] != 0)
                ret.push_back(u8Ch[1]);
            if (u8Ch[2] != 0)
                ret.push_back(u8Ch[2]);
            if (u8Ch[3] != 0)
                ret.push_back(u8Ch[3]);
        }
        return ret;
    }
} // anonymous namespace

bool WAV::open(const filesystem::path& filename, bool convert_to_2ch_32bit) {
    close();
    file_path_ = filename;
    convert_to_2ch_32bit_ = convert_to_2ch_32bit;
    title_.clear();
    track_index_ = 0;
    end_of_stream_ = false;
    format_ = FormatConfigure();

    uint16_t channels, bits;
    uint64_t len;
    uint8_t s[10];
    file_.open(filename, ios::binary);

    if (!file_.is_open()) {
        cerr << "WAV: Failed to open file" << endl;
        return false;
    }

    // Read file header
    if (file_.read((char*)s, 4).fail()) {
        cerr << "WAV: Read error" << endl;
        return false;
    }
    s[4] = '\0';

    // Handle ID3 tags if present
    if (memcmp(s, "ID3", 3) == 0) {
        if (s[3] == 3 || s[3] == 4) {  // ID3v2.3 or v2.4
            int ver = s[3];
            file_.read((char*)s, 2);  // skip
            if (s[1] & (1 << 1)) {
                cerr << "WAV: ID3 extended header unsupported" << endl;
                return false;
            }
            file_.read((char*)s, 4);
            uint32_t tag_len = (((uint8_t)s[0]) << 21) + (((uint8_t)s[1]) << 14) +
                               (((uint8_t)s[2]) << 7) + (uint8_t)s[3];

            while (tag_len > 0) {
                tag_len -= 4;
                file_.read((char*)s, 4);  // Frame ID
                s[4] = '\0';

                if (s[0] == '\0') {
                    file_.seekg(tag_len, file_.cur);
                    break;
                }

                if (s[0] & 0x80 || s[1] & 0x80 || s[2] & 0x80 || s[3] & 0x80) {
                    file_.seekg(tag_len, file_.cur);
                    break;
                }

                bool is_title = (memcmp(s, "TIT2", 4) == 0);
                bool is_track = (memcmp(s, "TRCK", 4) == 0);

                file_.read((char*)s, 4);  // size
                tag_len -= 4;

                uint32_t frame_len = 0;
                if (ver == 4) {
                    frame_len = (((uint8_t)s[0]) << 21) + (((uint8_t)s[1]) << 14) +
                               (((uint8_t)s[2]) << 7) + (uint8_t)s[3];
                } else {
                    frame_len = (((uint8_t)s[0]) << 24) + (((uint8_t)s[1]) << 16) +
                               (((uint8_t)s[2]) << 8) + (uint8_t)s[3];
                }

                if ((tag_len - 2) < frame_len) {
                    file_.seekg(tag_len, file_.cur);
                    break;
                }

                file_.read((char*)s, 2);  // flags
                tag_len -= 2;

                if (tag_len >= 2) {
                    string str;
                    str.resize(frame_len - 1);
                    file_.read((char*)s, 1);  // encoding
                    file_.read((char*)&str.front(), str.size());

                    if (s[0] == 0 || s[0] == 3) {
                        if (is_title) {
                            title_ = str;
                        }
                        if (is_track) {
                            string::size_type f = str.find('/');
                            if (f != string::npos) {
                                str.resize(f);
                            }
                            track_index_ = atoi(str.c_str());
                        }
                    }
                } else {
                    file_.seekg(frame_len, file_.cur);
                }
                tag_len -= frame_len;
            }
            file_.read((char*)s, 4);
            s[4] = '\0';
        } else {
            cerr << "WAV: Unsupported ID3 version" << endl;
            return false;
        }
    }

    s[4] = '\0';

    // Identify file format and parse header
    if (memcmp(s, "RIFF", 4) == 0) {
        mode_ = FormatMode::PCM;
        read4bytes();  // RIFF size

        // WAVEfmt?
        if (file_.read((char*)s, 8).fail()) {
            cerr << "WAV: Read error" << endl;
            return false;
        }
        if (memcmp(s, "WAVEfmt ", 8) != 0) {
            cerr << "WAV: Not a WAVEfmt format" << endl;
            return false;
        }

        // Chunk Length
        len = read4bytes();
        if (len < 16) {
            cerr << "WAV: Length of WAVEfmt must be at least 16" << endl;
            return false;
        }
        uint64_t rest = len - 16;

        // Format info
        uint16_t type = read2bytes();
        channels = read2bytes();
        int sampling_rate = read4bytes();
        read4bytes();  // bytes per second
        int bytes_per_sample = read2bytes();
        bits = read2bytes();

        // Set format
        format_.setChannel(channels);

        if ((bytes_per_sample / channels) == 1)
            format_.setFormat(FormatID::FMT_PCM_SIGNED_8);
        if ((bytes_per_sample / channels) == 2)
            format_.setFormat(FormatID::FMT_PCM_SIGNED_16);
        if ((bytes_per_sample / channels) == 3)
            format_.setFormat(FormatID::FMT_PCM_SIGNED_24);
        if ((bytes_per_sample / channels) == 4) {
            if (type == 3)
                format_.setFormat(FormatID::FMT_PCM_FLOAT_32);
            else
                format_.setFormat(FormatID::FMT_PCM_SIGNED_32);
        }
        format_.setSpeed(sampling_rate);

        format_2ch_32bit_ = format_;
        if (convert_to_2ch_32bit) {
            convert_to_2ch_32bit_ = false;
            if (format_2ch_32bit_.isPCM()) {
                if (format_2ch_32bit_.isSigned()) {
                    if (format_2ch_32bit_.getBits() < 32) {
                        if (format_2ch_32bit_.getChannel() <= 2) {
                            convert_to_2ch_32bit_ = true;
                            format_2ch_32bit_.setFormat(FormatID::FMT_PCM_SIGNED_32);
                            format_2ch_32bit_.setChannel(2);
                        }
                    }
                }
            }
        }

        file_.seekg(rest, file_.cur);

        pcm_data_remaining_ = 0;
        dsd_block_size_ = 0;
        dsd_channel_count_ = 0;
        dsd_buffer_remaining_ = 0;
        dsd_data_remaining_ = 0;

        // Parse LIST INFO for metadata
        uint8_t chunk_id[5];
        uint64_t file_offset = file_.tellg();
        while (!file_.read((char*)chunk_id, 4).fail()) {
            len = read4bytes();
            chunk_id[4] = 0;

            if (memcmp(chunk_id, "LIST", 4) == 0) {
                if (len >= 4) {
                    file_.read((char*)chunk_id, 4);
                    chunk_id[4] = 0;
                    len -= 4;

                    if (memcmp(chunk_id, "INFO", 4) == 0) {
                        while (true) {
                            if (len >= 4) {
                                file_.read((char*)chunk_id, 4);
                                chunk_id[4] = 0;
                                len -= 4;
                                int detail = 0;

                                if (len >= 4) {
                                    if (memcmp(chunk_id, "INAM", 4) == 0) detail = 1;
                                    if (memcmp(chunk_id, "ITRK", 4) == 0) detail = 6;

                                    if (detail != 0) {
                                        uint32_t info_size = read4bytes();
                                        if (info_size != 0 && len >= info_size) {
                                            Buffer info;
                                            info.resize(info_size);
                                            file_.read(info.get_char(), info_size);
                                            len -= info_size;

                                            if (detail == 1) {  // title
                                                title_ = info.get_string();
                                            }
                                            if (detail == 6) {  // track
                                                if (info.size() == 2) {
                                                    track_index_ = uint16_t(info[0]) | (uint16_t(info[1]) << 8);
                                                }
                                            }
                                        } else {
                                            file_.seekg(len, file_.cur);
                                            break;
                                        }
                                    } else {
                                        file_.seekg(len, file_.cur);
                                        break;
                                    }
                                } else {
                                    file_.seekg(len, file_.cur);
                                    break;
                                }
                            } else {
                                file_.seekg(len, file_.cur);
                            }
                        }
                    } else {
                        file_.seekg(len, file_.cur);
                    }
                } else {
                    file_.seekg(len, file_.cur);
                }
            } else {
                file_.seekg(len, file_.cur);
            }
        }
        file_.clear();
        file_.seekg(file_offset, file_.beg);

    } else if (memcmp(s, "DSD ", 4) == 0) {
        // DSF format handling
        convert_to_2ch_32bit_ = false;
        mode_ = FormatMode::DSF;

        uint64_t chunk_size = read8bytes();
        if (chunk_size != 28) {
            cerr << "WAV: DSF chunk size must be 28" << endl;
            return false;
        }

        uint64_t file_size = read8bytes();
        uint64_t pointer = read8bytes();

        if (file_.read((char*)s, 4).fail()) {
            cerr << "WAV: Read error" << endl;
            return false;
        }
        if (memcmp(s, "fmt ", 4) != 0) {
            cerr << "WAV: Not a DSF fmt format" << endl;
            return false;
        }

        uint64_t format_size = read8bytes();
        if (format_size != 52) {
            cerr << "WAV: DSF format size must be 52" << endl;
            return false;
        }

        uint32_t version = read4bytes();
        uint32_t format_id = read4bytes();
        uint32_t channel_type = read4bytes();
        uint32_t ch = read4bytes();
        uint32_t hz = read4bytes();
        uint32_t bit = read4bytes();
        if (bit != 1) {
            cerr << "WAV: DSF bit must be 1" << endl;
        }
        uint64_t samples = read8bytes();
        uint32_t block = read4bytes();
        read4bytes();  // reserved

        format_.setChannel(ch);
        format_.setFormat(FormatID::FMT_DSD1 | FormatID::FMT_DSD_SIZ_32 |
                         FormatID::FMT_DSD_MSB | FormatID::FMT_DSD_LITTLE);
        format_.setSpeed(hz);

        pcm_data_remaining_ = 0;
        dsd_samples_remaining_ = samples;
        dsd_block_size_ = block;
        dsd_channel_count_ = ch;
        dsd_buffer_.resize(block * ch);
        dsd_buffer_remaining_ = 0;
        dsd_data_remaining_ = 0;

        // Parse metadata tags
        uint8_t tag_id[5];
        uint64_t file_offset = file_.tellg();
        while (!file_.read((char*)tag_id, 4).fail()) {
            len = read8bytes();
            len -= 12;
            tag_id[4] = 0;

            if (memcmp(tag_id, "ID3", 3) == 0) {
                len += 12;
                tag_id[0] = ((const uint8_t*)&len)[6];
                tag_id[1] = ((const uint8_t*)&len)[7];
                const uint8_t* p = ((const uint8_t*)&len) + 2;
                uint32_t val = (((uint8_t)p[0]) << 21) + (((uint8_t)p[1]) << 14) +
                              (((uint8_t)p[2]) << 7) + (uint8_t)p[3];
                len = val;
                if (len < 2) {
                    return false;
                }
                len -= 2;
                bool first = true;

                if (tag_id[3] == 3) {  // ID3v2.3
                    while (len > 0) {
                        if (first) {
                            first = false;
                            if (len < 2) {
                                file_.seekg(len, file_.cur);
                                break;
                            }
                            file_.read((char*)tag_id + 2, 2);
                            len -= 2;
                            tag_id[4] = 0;
                        } else {
                            if (len < 4) {
                                file_.seekg(len, file_.cur);
                                break;
                            }
                            file_.read((char*)tag_id, 4);
                            len -= 4;
                            tag_id[4] = 0;
                        }

                        uint32_t frame_size = read4bytes();
                        len -= 4;
                        frame_size = (uint32_t(((const uint8_t*)&frame_size)[0]) << 24) |
                                    (uint32_t(((const uint8_t*)&frame_size)[1]) << 16) |
                                    (uint32_t(((const uint8_t*)&frame_size)[2]) << 8) |
                                    (uint32_t(((const uint8_t*)&frame_size)[3]) << 0);
                        frame_size += 2;

                        if (frame_size > len) {
                            file_.seekg(len, file_.cur);
                            break;
                        }

                        read2bytes();  // flags
                        frame_size -= 2;
                        len -= 2;

                        if (frame_size > 0) {
                            uint8_t encoding = read1byte();
                            frame_size -= 1;
                            len -= 1;

                            if (frame_size > 0) {
                                Buffer info;
                                info.resize(frame_size);
                                file_.read(info.get_char(), frame_size);

                                if (encoding == 0 || encoding == 3) {  // ASCII or UTF-8
                                    if (memcmp(tag_id, "TIT2", 4) == 0) {
                                        title_ = info.get_string();
                                    }
                                    if (memcmp(tag_id, "TRCK", 4) == 0) {
                                        track_index_ = atoi(info.get_string().c_str());
                                    }
                                }
                                len -= frame_size;
                            }
                        }
                    }
                } else {
                    file_.seekg(len, file_.cur);
                }
            } else {
                file_.seekg(len, file_.cur);
            }
        }
        file_.clear();
        file_.seekg(file_offset, file_.beg);

    } else if (memcmp(s, "FRM8", 4) == 0) {
        // DSDIFF format
        convert_to_2ch_32bit_ = false;
        dff_state_.chunk_size = read8bytesBIG();
        dff_state_.type = read4bytes();
        dff_state_.chunk_size -= 4;

        DFFState dff_backup = dff_state_;
        bool finished = false;

        auto read_func = [&](std::ifstream& file, std::size_t& len) {
            if (len == 0) {
                finished = true;
                return true;
            }
            file.seekg(len, file.cur);
            len = 0;
            return true;
        };

        while (!finished) {
            if (!readDFFChunk(read_func))
                return false;
        }

        format_.setFormat(FormatID::FMT_DSD1 | FormatID::FMT_DSD_SIZ_32 |
                         FormatID::FMT_DSD_MSB | FormatID::FMT_DSD_LITTLE);
        mode_ = FormatMode::DFF;

        file_.clear();
        file_.seekg(4 + 8 + 4, file_.beg);
        dff_state_ = dff_backup;

    } else if (memcmp(s, "FORM", 4) == 0) {
        // AIFF format
        mode_ = FormatMode::AIFF;
        uint32_t chunk_size = read4bytesBIG();
        uint32_t form_type = read4bytesBIG();

        uint32_t common_id = read4bytesBIG();
        uint32_t common_size = read4bytesBIG();

        if (form_type != 0x41494646 || common_id != 0x434F4D4D) {
            cerr << "WAV: AIFF common chunk error" << endl;
            return false;
        }

        uint16_t ch = read2bytesBIG();
        uint32_t frames = read4bytesBIG();
        uint32_t bit = read2bytesBIG();
        uint16_t hz_exp = read2bytesBIG();
        uint64_t hz_frac = read8bytesBIG();

        double hz_f = ((double)hz_frac);
        for (int a = 0; a < 63; ++a)
            hz_f /= 2;
        uint16_t hz_exp_plus = (hz_exp & 0x7FFF) - ((1 << 14) - 1);
        for (int a = 0; a < hz_exp_plus; ++a)
            hz_f *= 2;
        uint32_t hz = hz_f;

        format_.setChannel(ch);

        if (bit == 8)
            format_.setFormat(FormatID::FMT_PCM_SIGNED_8);
        if (bit == 16)
            format_.setFormat(FormatID::FMT_PCM_SIGNED_16);
        if (bit == 24)
            format_.setFormat(FormatID::FMT_PCM_SIGNED_24);
        if (bit == 32)
            format_.setFormat(FormatID::FMT_PCM_SIGNED_32);

        format_.setSpeed(hz);

        format_2ch_32bit_ = format_;
        if (convert_to_2ch_32bit) {
            convert_to_2ch_32bit_ = false;
            if (format_2ch_32bit_.isPCM()) {
                if (format_2ch_32bit_.isSigned()) {
                    if (format_2ch_32bit_.getBits() < 32) {
                        if (format_2ch_32bit_.getChannel() <= 2) {
                            convert_to_2ch_32bit_ = true;
                            format_2ch_32bit_.setFormat(FormatID::FMT_PCM_SIGNED_32);
                            format_2ch_32bit_.setChannel(2);
                        }
                    }
                }
            }
        }

        uint64_t file_offset = file_.tellg();

        while (!file_.read((char*)s, 4).fail()) {
            uint32_t chunk_len = read4bytesBIG();

            if (memcmp(s, "SSND", 4) == 0) {
                file_.seekg(chunk_len, file_.cur);
            } else if (memcmp(s, "ID3 ", 4) == 0) {
                file_.read((char*)s, 3);  // dummy
                file_.read((char*)s, 3);
                chunk_len -= 6;

                if (s[0] == 3) {  // ID3v2.3
                    file_.read((char*)s, 4);
                    uint32_t tag_len = (((uint8_t)s[0]) << 21) + (((uint8_t)s[1]) << 14) +
                                      (((uint8_t)s[2]) << 7) + (uint8_t)s[3];

                    while (tag_len > 0) {
                        if (tag_len < 4)
                            return false;
                        file_.read((char*)s, 4);
                        s[4] = '\0';
                        tag_len -= 4;

                        if (tag_len < 7)
                            return false;

                        int size = read4bytesBIG();
                        size += 2;

                        if (tag_len < size)
                            return false;

                        size -= 2;
                        read2bytes();  // flags
                        uint8_t enc = read1byte();
                        size -= 1;
                        tag_len -= 7;

                        if (size > 0) {
                            Buffer info;
                            info.resize(size);
                            file_.read(info.get_char(), size);
                            tag_len -= size;

                            if (enc == 0 || enc == 3) {  // ASCII or UTF-8
                                if (memcmp(s, "TIT2", 4) == 0) {
                                    title_ = info.get_string();
                                }
                                if (memcmp(s, "TRCK", 4) == 0) {
                                    track_index_ = atoi(info.get_string().c_str());
                                }
                            } else if (enc == 1) {  // UTF-16
                                u16string s16;
                                s16.resize(info.size() / 2);
                                memcpy(&s16.front(), info.get(), s16.size() * 2);
                                u8string s8 = utf16utf8(s16);

                                if (memcmp(s, "TIT2", 4) == 0) {
                                    title_ = (const char*)s8.c_str();
                                }
                                if (memcmp(s, "TRCK", 4) == 0) {
                                    track_index_ = atoi((const char*)s8.c_str());
                                }
                            }
                        }
                    }
                }
                file_.seekg(chunk_len, file_.cur);
            } else {
                file_.seekg(chunk_len, file_.cur);
            }
        }
        file_.clear();
        file_.seekg(file_offset, file_.beg);
        pcm_data_remaining_ = 0;
        dsd_buffer_remaining_ = 0;

    } else {
        // Check for M4A/ALAC format
        int64_t size = s[3];
        size -= 4;
        file_.read((char*)s, 4);
        size -= 4;

        if (memcmp(s, "ftyp", 4) == 0) {
            // QuickTime / ALAC
            file_.seekg(size, file_.cur);
            size = read4bytesBIG();

            if (size == 0x00000001) {
                size = read8bytesBIG();
                size -= 8;
            }
            size -= 4;

            while (!file_.fail()) {
                file_.read((char*)s, 4);
                size -= 4;

                if (memcmp(s, "moov", 4) == 0) {
                    size -= readChildM4A(size);
                } else if (memcmp(s, "mdat", 4) == 0) {
                    // data
                }

                file_.seekg(size, file_.cur);
                size = read4bytesBIG();

                if (size == 0x00000001) {
                    size = read8bytesBIG();
                    size -= 8;
                }
                size -= 4;
            }
            return true;
        } else {
            cerr << "WAV: Not a recognized format" << endl;
            return false;
        }
    }

    // Extract track index from title or filename if not set
    if (track_index_ == 0) {
        if (title_.size() >= 2) {
            if (title_[0] >= '0' && title_[0] <= '9') {
                int i = title_[0] - '0';
                if ((title_[1] >= '0' && title_[1] <= '9')) {
                    i *= 10;
                    i += title_[1] - '0';
                }
                track_index_ = i;
            }
        }

        if (track_index_ == 0) {
            string stem = (const char*)(filename.stem().u8string().c_str());
            if (stem.size() >= 2) {
                if (stem[0] >= '0' && stem[0] <= '9') {
                    int i = stem[0] - '0';
                    if ((stem[1] >= '0' && stem[1] <= '9')) {
                        i *= 10;
                        i += stem[1] - '0';
                    }
                    track_index_ = i;
                }
            }
        }
    }

    if (title_.empty()) {
        title_ = (const char*)(filename.stem().u8string().c_str());
    }

    return true;
}

int64_t WAV::readChildM4A(int64_t remaining_size) {
    char s[5];
    s[4] = '\0';
    int read_size = 0;

    while (read_size < remaining_size) {
        uint32_t child_size = read4bytesBIG();
        read_size += 4;
        child_size -= 4;
        file_.read((char*)s, 4);
        read_size += 4;
        child_size -= 4;

        if (memcmp(s, "trak", 4) == 0 || memcmp(s, "mdia", 4) == 0 ||
            memcmp(s, "minf", 4) == 0 || memcmp(s, "stbl", 4) == 0 ||
            memcmp(s, "udta", 4) == 0 || memcmp(s, "ilst", 4) == 0) {
            int64_t rs = readChildM4A(child_size);
            child_size -= rs;
            read_size += rs;
        } else {
            if (memcmp(s, "meta", 4) == 0) {
                uint32_t type = read4bytesBIG();
                child_size -= 4;
                read_size += 4;

                if (type == 0) {
                    int64_t rs = readChildM4A(child_size);
                    child_size -= rs;
                    read_size += rs;
                }
            } else if (memcmp(s, "ilst", 4) == 0) {
                int64_t rs = readChildM4A(child_size);
                child_size -= rs;
                read_size += rs;
            } else if (memcmp(s, "\xa9nam", 4) == 0 || memcmp(s, "trkn", 4) == 0) {
                bool is_title = memcmp(s, "\xa9nam", 4) == 0;
                uint32_t meta_size = read4bytesBIG();
                meta_size -= 4;
                read_size += 4;
                child_size -= 4;
                file_.read((char*)s, 4);
                meta_size -= 4;
                read_size += 4;
                child_size -= 4;
                read4bytesBIG();  // ???
                meta_size -= 4;
                read_size += 4;
                child_size -= 4;
                read4bytesBIG();  // ???
                meta_size -= 4;
                read_size += 4;
                child_size -= 4;

                if (memcmp(s, "data", 4) == 0) {
                    if (is_title) {
                        string str;
                        str.resize(meta_size);
                        file_.read((char*)&str.front(), str.size());
                        read_size += meta_size;
                        child_size -= meta_size;
                        title_ = str;
                    } else {
                        uint32_t track_no = read4bytesBIG();
                        meta_size -= 4;
                        read_size += 4;
                        child_size -= 4;
                        file_.seekg(meta_size, file_.cur);
                        read_size += meta_size;
                        child_size -= meta_size;
                        track_index_ = track_no;
                    }
                }
            }
        }
        file_.seekg(child_size, file_.cur);
        read_size += child_size;
    }
    return read_size;
}

bool WAV::read(ACQUA::Buffer& buffer, std::size_t target_bytes, ReadRest& rest) {
    switch (mode_) {
        case FormatMode::PCM:
            return readPCM(buffer, target_bytes);
        case FormatMode::DSF:
            return readDSF(buffer, target_bytes, rest);
        case FormatMode::DFF:
            return readDFF(buffer, target_bytes, rest);
        case FormatMode::AIFF:
            return readAIFF(buffer, target_bytes);
        default:
            return false;
    }
}

bool WAV::readPCM(Buffer& buffer, size_t target_bytes) {
    if (convert_to_2ch_32bit_) {
        target_bytes *= format_.getFrameSize();
        target_bytes /= format_2ch_32bit_.getFrameSize();
    }

    // Read until 'data' chunk
    if (pcm_data_remaining_ == 0) {
        bool found = false;
        uint8_t s[5];

        while (!file_.read((char*)s, 4).fail()) {
            pcm_data_remaining_ = read4bytes();
            s[4] = 0;

            if (memcmp(s, "data", 4) == 0) {
                found = true;
                break;
            } else {
                file_.seekg(pcm_data_remaining_, file_.cur);
            }
        }

        if (!found) {
            buffer.clear();
            end_of_stream_ = true;
            return true;
        }
    }

    if (target_bytes > pcm_data_remaining_)
        target_bytes = pcm_data_remaining_;

    buffer.resize(target_bytes);
    if (file_.read(buffer.get_char(), target_bytes).fail()) {
        buffer.clear();
        cerr << "WAV: Read error" << endl;
        return false;
    }
    pcm_data_remaining_ -= uint32_t(target_bytes);

    if (convert_to_2ch_32bit_) {
        Buffer tmp;
        size_t frame_count = buffer.size() / format_.getWid();

        if (format_.getChannel() == 1) {
            tmp.resize(frame_count * 8);

            if (format_.getWid() == 1) {
                for (size_t a = 0; a < frame_count; ++a) {
                    uint32_t t = buffer.get_8()[a];
                    t <<= 24;
                    tmp.get_32()[a * 2 + 0] = t;
                    tmp.get_32()[a * 2 + 1] = t;
                }
            }
            if (format_.getWid() == 2) {
                for (size_t a = 0; a < frame_count; ++a) {
                    uint32_t t = buffer.get_16()[a];
                    t <<= 16;
                    tmp.get_32()[a * 2 + 0] = t;
                    tmp.get_32()[a * 2 + 1] = t;
                }
            }
            if (format_.getWid() == 3) {
                for (size_t a = 0; a < frame_count; ++a) {
                    uint32_t t = buffer.get_8()[a * format_.getWid() + 2];
                    t <<= 8;
                    t |= buffer.get_8()[a * format_.getWid() + 1];
                    t <<= 8;
                    t |= buffer.get_8()[a * format_.getWid() + 0];
                    t <<= 8;
                    tmp.get_32()[a * 2 + 0] = t;
                    tmp.get_32()[a * 2 + 1] = t;
                }
            }
        } else {
            tmp.resize(frame_count * 4);

            if (format_.getWid() == 1) {
                for (size_t a = 0; a < frame_count; ++a) {
                    uint32_t t = buffer.get_8()[a];
                    t <<= 24;
                    tmp.get_32()[a] = t;
                }
            }
            if (format_.getWid() == 2) {
                for (size_t a = 0; a < frame_count; ++a) {
                    uint32_t t = buffer.get_16()[a];
                    t <<= 16;
                    tmp.get_32()[a] = t;
                }
            }
            if (format_.getWid() == 3) {
                for (size_t a = 0; a < frame_count; ++a) {
                    uint32_t t = buffer.get_8()[a * format_.getWid() + 2];
                    t <<= 8;
                    t |= buffer.get_8()[a * format_.getWid() + 1];
                    t <<= 8;
                    t |= buffer.get_8()[a * format_.getWid() + 0];
                    t <<= 8;
                    tmp.get_32()[a] = t;
                }
            }
        }
        tmp.swap(buffer);
    }

    return true;
}

bool WAV::readDSF(Buffer& buffer, size_t target_bytes, ReadRest& rest) {
    buffer.resize(target_bytes);
    uint32_t* output = buffer.get_32();
    size_t written_size = 0;

    while (target_bytes > 0) {
        // Read until 'data' chunk
        if (dsd_data_remaining_ == 0) {
            bool found = false;
            uint8_t s[5];

            while (!file_.read((char*)s, 4).fail()) {
                dsd_data_remaining_ = read8bytes();
                if (dsd_data_remaining_ < 12)
                    return false;
                dsd_data_remaining_ -= 12;

                s[4] = 0;

                if (memcmp(s, "data", 4) == 0) {
                    found = true;
                    break;
                } else if (memcmp(s, "ID3", 3) == 0) {
                    dsd_data_remaining_ += 12;
                    const uint8_t* p = ((const uint8_t*)&dsd_data_remaining_) + 2;
                    uint32_t val = (((uint8_t)p[0]) << 21) + (((uint8_t)p[1]) << 14) +
                                  (((uint8_t)p[2]) << 7) + (uint8_t)p[3];
                    dsd_data_remaining_ = val;
                    if (dsd_data_remaining_ < 2) {
                        return false;
                    }
                    dsd_data_remaining_ -= 2;
                    file_.seekg(dsd_data_remaining_, file_.cur);
                } else {
                    file_.seekg(dsd_data_remaining_, file_.cur);
                }
            }

            if (!found) {
                buffer.resize(written_size);
                end_of_stream_ = true;
                return true;
            }
        }

        if (dsd_buffer_remaining_ == 0) {
            if (dsd_data_remaining_ < dsd_block_size_ * dsd_channel_count_) {
                buffer.clear();
                cerr << "WAV: Read error" << endl;
                end_of_stream_ = true;
                return false;
            }

            if (file_.read(dsd_buffer_.get_char(), dsd_block_size_ * dsd_channel_count_).fail()) {
                buffer.clear();
                cerr << "WAV: Read error" << endl;
                end_of_stream_ = true;
                return false;
            }
            dsd_data_remaining_ -= dsd_block_size_ * dsd_channel_count_;
            dsd_buffer_remaining_ = dsd_block_size_ * dsd_channel_count_;
        }

        size_t size = target_bytes;
        if (size > dsd_buffer_remaining_)
            size = dsd_buffer_remaining_;

        for (size_t a = 0; a < size / dsd_channel_count_; ++a) {
            const size_t offset = (dsd_buffer_.size() - dsd_buffer_remaining_) / dsd_channel_count_;

            if (dsd_samples_remaining_ < 8) {
                if (dsd_samples_remaining_ != 0) {
                    uint8_t tmp[ReadRest::MAX_CHANNELS];
                    for (int c = 0; c < dsd_channel_count_; ++c) {
                        tmp[c] = dsd_buffer_.get()[dsd_block_size_ * c + offset];
                    }
                    rest.push8_lsb(tmp, dsd_samples_remaining_);
                    dsd_buffer_remaining_ = 0;
                }
                break;
            }

            uint8_t tmp[ReadRest::MAX_CHANNELS];
            for (int c = 0; c < dsd_channel_count_; ++c) {
                tmp[c] = dsd_buffer_.get()[dsd_block_size_ * c + offset];
            }
            rest.push8_lsb(tmp);

            if (rest.full(output)) {
                output += dsd_channel_count_;
                written_size += 4 * dsd_channel_count_;
                target_bytes -= 4 * dsd_channel_count_;
            }

            dsd_buffer_remaining_ -= dsd_channel_count_;
            dsd_samples_remaining_ -= 8;
        }
    }
    return true;
}

bool WAV::readDFF(ACQUA::Buffer& buffer, size_t target_bytes, ReadRest& rest) {
    auto read_func = [&](std::ifstream& file, std::size_t& len) {
        if (len == 0) {
            buffer.clear();
            return true;
        }
        if ((len % format_.getChannel()) != 0)
            return false;
        if ((target_bytes % (format_.getChannel() * 4)) != 0)
            return false;
        if (len < target_bytes)
            target_bytes = len;

        Buffer tmp;
        buffer.resize(target_bytes);
        tmp.resize(target_bytes);
        file.read(tmp.get_char(), tmp.size());
        const size_t ch = format_.getChannel();
        uint8_t* tmp_data = tmp.get();

        uint32_t* output = buffer.get_32();
        for (size_t a = 0; a < target_bytes / ch; ++a) {
            rest.push8_msb(tmp_data);
            tmp_data += ch;
            if (rest.full(output)) {
                output += ch;
            }
        }
        len -= target_bytes;
        return true;
    };

    if (!readDFFChunk(read_func))
        return false;
    return true;
}

bool WAV::processDFF(std::function<bool(std::ifstream&, std::size_t&)> read_func) {
    uint64_t original = dff_state_.read_reset;
    if (!read_func(file_, dff_state_.read_reset))
        return false;
    uint64_t used = original - dff_state_.read_reset;
    dff_state_.current_size -= used;
    dff_state_.chunk_size -= used;
    return true;
}

bool WAV::readDFFChunk(std::function<bool(std::ifstream&, std::size_t&)> read_func) {
    if (dff_state_.read_reset != 0) {
        if (!processDFF(read_func))
            return false;
        return true;
    }

    char s[5];
    while (dff_state_.chunk_size > 0 && !file_.read((char*)s, 4).fail()) {
        dff_state_.chunk_size -= 4;
        s[4] = '\0';

        if (dff_state_.chunk_size < 8) {
            return false;
        }

        dff_state_.current_size = read8bytesBIG();
        dff_state_.chunk_size -= 8;

        if (dff_state_.chunk_size < dff_state_.current_size) {
            return false;
        }

        if (memcmp(s, "FVER", 4) == 0) {
            if (dff_state_.current_size < 4)
                return false;
            uint32_t version = read4bytesBIG();
            dff_state_.current_size -= 4;
            dff_state_.chunk_size -= 4;

        } else if (memcmp(s, "PROP", 4) == 0) {
            if (dff_state_.current_size < 4)
                return false;
            uint32_t type = read4bytes();
            dff_state_.current_size -= 4;
            dff_state_.chunk_size -= 4;

            while (dff_state_.current_size > 0 && !file_.read((char*)s, 4).fail()) {
                dff_state_.chunk_size -= 4;
                dff_state_.current_size -= 4;
                s[4] = '\0';

                if (dff_state_.chunk_size < 8 || dff_state_.current_size < 8)
                    return false;

                uint64_t size = read8bytesBIG();
                dff_state_.chunk_size -= 8;
                dff_state_.current_size -= 8;

                if (dff_state_.current_size < size)
                    return false;

                if (memcmp(s, "FS  ", 4) == 0) {
                    if (size < 4)
                        return false;
                    uint32_t hz = read4bytesBIG();
                    size -= 4;
                    dff_state_.current_size -= 4;
                    dff_state_.chunk_size -= 4;
                    format_.setSpeed(hz);

                } else if (memcmp(s, "CHNL", 4) == 0) {
                    if (size < 2)
                        return false;
                    uint32_t ch = read2bytesBIG();
                    size -= 2;
                    dff_state_.current_size -= 2;
                    dff_state_.chunk_size -= 2;
                    format_.setChannel(ch);

                } else if (memcmp(s, "CMPR", 4) == 0) {
                    // skip
                } else if (memcmp(s, "ABSS", 4) == 0) {
                    // skip
                } else if (memcmp(s, "LSCO", 4) == 0) {
                    // skip
                }

                file_.seekg(size, file_.cur);
                dff_state_.chunk_size -= size;
                dff_state_.current_size -= size;
            }

        } else if (memcmp(s, "DSD ", 4) == 0) {
            dff_state_.read_reset = dff_state_.current_size;
            if (dff_state_.read_reset != 0) {
                if (!processDFF(read_func))
                    return false;
                return true;
            }

        } else if (memcmp(s, "COMT", 4) == 0) {
            // skip
        } else if (memcmp(s, "DIIN", 4) == 0) {
            // skip
        } else if (memcmp(s, "DST ", 4) == 0) {
            // skip
        } else if (memcmp(s, "MANF", 4) == 0) {
            // skip
        } else if (memcmp(s, "ID3 ", 4) == 0) {
            if (dff_state_.current_size < 3)
                return false;
            dff_state_.current_size -= 3;
            dff_state_.chunk_size -= 3;
            file_.read((char*)s, 3);
            s[3] = '\0';

            if (memcmp(s, "ID3", 3) == 0) {
                if (dff_state_.current_size < 7)
                    return false;
                file_.read((char*)s, 3);  // dummy

                if (s[0] == 3) {  // ID3v2.3
                    file_.read((char*)s, 4);
                    dff_state_.current_size -= 7;
                    dff_state_.chunk_size -= 7;
                    uint32_t len = (((uint8_t)s[0]) << 21) + (((uint8_t)s[1]) << 14) +
                                  (((uint8_t)s[2]) << 7) + (uint8_t)s[3];

                    if (dff_state_.current_size < len)
                        return false;

                    while (len > 0) {
                        if (len < 4)
                            return false;
                        file_.read((char*)s, 4);
                        s[4] = '\0';
                        len -= 4;
                        dff_state_.current_size -= 4;
                        dff_state_.chunk_size -= 4;

                        if (len < 7)
                            return false;

                        int size = read4bytesBIG();
                        size += 2;

                        if (len < size)
                            return false;

                        size -= 2;
                        read2bytes();  // flags
                        uint8_t enc = read1byte();
                        size -= 1;
                        len -= 7;
                        dff_state_.current_size -= 7;
                        dff_state_.chunk_size -= 7;

                        if (size > 0) {
                            Buffer info;
                            info.resize(size);
                            file_.read(info.get_char(), size);

                            if (enc == 0 || enc == 3) {  // ASCII or UTF-8
                                if (memcmp(s, "TIT2", 4) == 0) {
                                    title_ = info.get_string();
                                }
                                if (memcmp(s, "TRCK", 4) == 0) {
                                    track_index_ = atoi(info.get_string().c_str());
                                }
                            }
                            len -= size;
                            dff_state_.current_size -= size;
                            dff_state_.chunk_size -= size;
                        }
                    }
                }
            }
        }

        file_.seekg(dff_state_.current_size, file_.cur);
        dff_state_.chunk_size -= dff_state_.current_size;
    }

    dff_state_.current_size = 0;
    if (!read_func(file_, dff_state_.current_size))
        return false;
    return true;
}

bool WAV::readAIFF(Buffer& buffer, size_t target_bytes) {
    if (convert_to_2ch_32bit_) {
        target_bytes *= format_.getFrameSize();
        target_bytes /= format_2ch_32bit_.getFrameSize();
    }

    // Read until 'SSND' chunk
    if (pcm_data_remaining_ == 0) {
        bool found = false;
        uint8_t s[5];

        while (!file_.read((char*)s, 4).fail()) {
            pcm_data_remaining_ = read4bytesBIG();
            s[4] = 0;

            if (memcmp(s, "SSND", 4) == 0) {
                found = true;
                break;
            } else {
                file_.seekg(pcm_data_remaining_, file_.cur);
            }
        }

        if (!found) {
            buffer.clear();
            end_of_stream_ = true;
            return true;
        }
    }

    if (target_bytes > pcm_data_remaining_)
        target_bytes = pcm_data_remaining_;

    buffer.resize(target_bytes);
    if (file_.read(buffer.get_char(), target_bytes).fail()) {
        buffer.clear();
        cerr << "WAV: Read error" << endl;
        return false;
    }
    pcm_data_remaining_ -= uint32_t(target_bytes);

    // Swap bytes for big-endian format
    for (int a = 0; a < buffer.size() / format_.getWid(); ++a) {
        uint8_t tmp[4];

        if (format_.getWid() == 2) {
            tmp[0] = buffer[a * 2 + 0];
            tmp[1] = buffer[a * 2 + 1];
            buffer[a * 2 + 0] = tmp[1];
            buffer[a * 2 + 1] = tmp[0];
        }
        if (format_.getWid() == 3) {
            tmp[0] = buffer[a * 3 + 0];
            tmp[1] = buffer[a * 3 + 1];
            tmp[2] = buffer[a * 3 + 2];
            buffer[a * 3 + 0] = tmp[2];
            buffer[a * 3 + 1] = tmp[1];
            buffer[a * 3 + 2] = tmp[0];
        }
        if (format_.getWid() == 4) {
            tmp[0] = buffer[a * 4 + 0];
            tmp[1] = buffer[a * 4 + 1];
            tmp[2] = buffer[a * 4 + 2];
            tmp[3] = buffer[a * 4 + 3];
            buffer[a * 4 + 0] = tmp[3];
            buffer[a * 4 + 1] = tmp[2];
            buffer[a * 4 + 2] = tmp[1];
            buffer[a * 4 + 3] = tmp[0];
        }
    }

    if (convert_to_2ch_32bit_) {
        Buffer tmp;
        size_t frame_count = buffer.size() / format_.getWid();

        if (format_.getChannel() == 1) {
            tmp.resize(frame_count * 8);

            if (format_.getWid() == 1) {
                for (size_t a = 0; a < frame_count; ++a) {
                    uint32_t t = buffer.get_8()[a];
                    t <<= 24;
                    tmp.get_32()[a * 2 + 0] = t;
                    tmp.get_32()[a * 2 + 1] = t;
                }
            }
            if (format_.getWid() == 2) {
                for (size_t a = 0; a < frame_count; ++a) {
                    uint32_t t = buffer.get_16()[a];
                    t <<= 16;
                    tmp.get_32()[a * 2 + 0] = t;
                    tmp.get_32()[a * 2 + 1] = t;
                }
            }
            if (format_.getWid() == 3) {
                for (size_t a = 0; a < frame_count; ++a) {
                    uint32_t t = buffer.get_8()[a * format_.getWid() + 2];
                    t <<= 8;
                    t |= buffer.get_8()[a * format_.getWid() + 1];
                    t <<= 8;
                    t |= buffer.get_8()[a * format_.getWid() + 0];
                    t <<= 8;
                    tmp.get_32()[a * 2 + 0] = t;
                    tmp.get_32()[a * 2 + 1] = t;
                }
            }
        } else {
            tmp.resize(frame_count * 4);

            if (format_.getWid() == 1) {
                for (size_t a = 0; a < frame_count; ++a) {
                    uint32_t t = buffer.get_8()[a];
                    t <<= 24;
                    tmp.get_32()[a] = t;
                }
            }
            if (format_.getWid() == 2) {
                for (size_t a = 0; a < frame_count; ++a) {
                    uint32_t t = buffer.get_16()[a];
                    t <<= 16;
                    tmp.get_32()[a] = t;
                }
            }
            if (format_.getWid() == 3) {
                for (size_t a = 0; a < frame_count; ++a) {
                    uint32_t t = buffer.get_8()[a * format_.getWid() + 2];
                    t <<= 8;
                    t |= buffer.get_8()[a * format_.getWid() + 1];
                    t <<= 8;
                    t |= buffer.get_8()[a * format_.getWid() + 0];
                    t <<= 8;
                    tmp.get_32()[a] = t;
                }
            }
        }
        tmp.swap(buffer);
    }

    return true;
}

void WAV::close() {
    file_.close();
    title_.clear();
    dsd_buffer_.clear();
}

// Binary read helpers (little-endian)
uint64_t WAV::read8bytes() {
    unsigned char buf[8];
    if (file_.read((char*)buf, 8).fail()) {
        return 0;
    }
    return ((((((buf[7] * 256LLU + buf[6]) * 256LLU + buf[5]) * 256LLU + buf[4]) * 256LLU + buf[3]) * 256LLU + buf[2]) * 256LLU + buf[1]) * 256LLU + buf[0];
}

uint32_t WAV::read4bytes() {
    unsigned char buf[4];
    if (file_.read((char*)buf, 4).fail()) {
        return 0;
    }
    return ((256LU * buf[3] + buf[2]) * 256LU + buf[1]) * 256LU + buf[0];
}

uint16_t WAV::read2bytes() {
    unsigned char buf[2];
    if (file_.read((char*)buf, 2).fail()) {
        return 0;
    }
    return (uint16_t(buf[1]) << 8) | uint16_t(buf[0]);
}

uint8_t WAV::read1byte() {
    unsigned char buf[1];
    if (file_.read((char*)buf, 1).fail()) {
        return 0;
    }
    return buf[0];
}

// Binary read helpers (big-endian)
uint64_t WAV::read8bytesBIG() {
    uint64_t tmp = read8bytes();
    uint64_t ret = 0;
    ret |= uint64_t(reinterpret_cast<uint8_t*>(&tmp)[0]);
    ret <<= 8;
    ret |= uint64_t(reinterpret_cast<uint8_t*>(&tmp)[1]);
    ret <<= 8;
    ret |= uint64_t(reinterpret_cast<uint8_t*>(&tmp)[2]);
    ret <<= 8;
    ret |= uint64_t(reinterpret_cast<uint8_t*>(&tmp)[3]);
    ret <<= 8;
    ret |= uint64_t(reinterpret_cast<uint8_t*>(&tmp)[4]);
    ret <<= 8;
    ret |= uint64_t(reinterpret_cast<uint8_t*>(&tmp)[5]);
    ret <<= 8;
    ret |= uint64_t(reinterpret_cast<uint8_t*>(&tmp)[6]);
    ret <<= 8;
    ret |= uint64_t(reinterpret_cast<uint8_t*>(&tmp)[7]);
    return ret;
}

uint32_t WAV::read4bytesBIG() {
    uint32_t tmp = read4bytes();
    uint32_t ret = 0;
    ret |= uint32_t(reinterpret_cast<uint8_t*>(&tmp)[0]);
    ret <<= 8;
    ret |= uint32_t(reinterpret_cast<uint8_t*>(&tmp)[1]);
    ret <<= 8;
    ret |= uint32_t(reinterpret_cast<uint8_t*>(&tmp)[2]);
    ret <<= 8;
    ret |= uint32_t(reinterpret_cast<uint8_t*>(&tmp)[3]);
    return ret;
}

uint16_t WAV::read2bytesBIG() {
    uint16_t tmp = read2bytes();
    uint16_t ret = 0;
    ret |= uint16_t(reinterpret_cast<uint8_t*>(&tmp)[0]);
    ret <<= 8;
    ret |= uint16_t(reinterpret_cast<uint8_t*>(&tmp)[1]);
    return ret;
}

// ReadRest implementation
const uint8_t WAV::ReadRest::SWAP_BITS_TABLE[256] = {
    0x00, 0x80, 0x40, 0xC0, 0x20, 0xA0, 0x60, 0xE0, 0x10, 0x90, 0x50, 0xD0, 0x30, 0xB0, 0x70, 0xF0,
    0x08, 0x88, 0x48, 0xC8, 0x28, 0xA8, 0x68, 0xE8, 0x18, 0x98, 0x58, 0xD8, 0x38, 0xB8, 0x78, 0xF8,
    0x04, 0x84, 0x44, 0xC4, 0x24, 0xA4, 0x64, 0xE4, 0x14, 0x94, 0x54, 0xD4, 0x34, 0xB4, 0x74, 0xF4,
    0x0C, 0x8C, 0x4C, 0xCC, 0x2C, 0xAC, 0x6C, 0xEC, 0x1C, 0x9C, 0x5C, 0xDC, 0x3C, 0xBC, 0x7C, 0xFC,
    0x02, 0x82, 0x42, 0xC2, 0x22, 0xA2, 0x62, 0xE2, 0x12, 0x92, 0x52, 0xD2, 0x32, 0xB2, 0x72, 0xF2,
    0x0A, 0x8A, 0x4A, 0xCA, 0x2A, 0xAA, 0x6A, 0xEA, 0x1A, 0x9A, 0x5A, 0xDA, 0x3A, 0xBA, 0x7A, 0xFA,
    0x06, 0x86, 0x46, 0xC6, 0x26, 0xA6, 0x66, 0xE6, 0x16, 0x96, 0x56, 0xD6, 0x36, 0xB6, 0x76, 0xF6,
    0x0E, 0x8E, 0x4E, 0xCE, 0x2E, 0xAE, 0x6E, 0xEE, 0x1E, 0x9E, 0x5E, 0xDE, 0x3E, 0xBE, 0x7E, 0xFE,
    0x01, 0x81, 0x41, 0xC1, 0x21, 0xA1, 0x61, 0xE1, 0x11, 0x91, 0x51, 0xD1, 0x31, 0xB1, 0x71, 0xF1,
    0x09, 0x89, 0x49, 0xC9, 0x29, 0xA9, 0x69, 0xE9, 0x19, 0x99, 0x59, 0xD9, 0x39, 0xB9, 0x79, 0xF9,
    0x05, 0x85, 0x45, 0xC5, 0x25, 0xA5, 0x65, 0xE5, 0x15, 0x95, 0x55, 0xD5, 0x35, 0xB5, 0x75, 0xF5,
    0x0D, 0x8D, 0x4D, 0xCD, 0x2D, 0xAD, 0x6D, 0xED, 0x1D, 0x9D, 0x5D, 0xDD, 0x3D, 0xBD, 0x7D, 0xFD,
    0x03, 0x83, 0x43, 0xC3, 0x23, 0xA3, 0x63, 0xE3, 0x13, 0x93, 0x53, 0xD3, 0x33, 0xB3, 0x73, 0xF3,
    0x0B, 0x8B, 0x4B, 0xCB, 0x2B, 0xAB, 0x6B, 0xEB, 0x1B, 0x9B, 0x5B, 0xDB, 0x3B, 0xBB, 0x7B, 0xFB,
    0x07, 0x87, 0x47, 0xC7, 0x27, 0xA7, 0x67, 0xE7, 0x17, 0x97, 0x57, 0xD7, 0x37, 0xB7, 0x77, 0xF7,
    0x0F, 0x8F, 0x4F, 0xCF, 0x2F, 0xAF, 0x6F, 0xEF, 0x1F, 0x9F, 0x5F, 0xDF, 0x3F, 0xBF, 0x7F, 0xFF
};

WAV::ReadRest::ReadRest(DIRETTA::FormatConfigure& format)
    : format_(format), channel_count_(format.getChannel()), bit_count_(0) {
    memset(rest_, format_.getMuteByte(), sizeof(rest_));
}

bool WAV::ReadRest::full(uint32_t* output) {
    if (bit_count_ < 32)
        return false;
    bit_count_ -= 32;
    for (int c = 0; c < channel_count_; ++c) {
        output[c] = rest_[c] >> bit_count_;
    }
    return true;
}

void WAV::ReadRest::final(ACQUA::Buffer& buffer) {
    if (bit_count_ == 0) {
        buffer.clear();
        return;
    }
    buffer.resize(4 * channel_count_);
    buffer.fill(format_.getMuteByte());
    for (int c = 0; c < channel_count_; ++c) {
        buffer.get_32()[c] &= (32 - bit_count_) - 1;
        buffer.get_32()[c] |= rest_[c] << (32 - bit_count_);
    }
}

void WAV::ReadRest::push8(const std::uint8_t* input) {
    bit_count_ += 8;
    for (int c = 0; c < channel_count_; ++c) {
        rest_[c] <<= 8;
        rest_[c] |= input[c];
    }
}

void WAV::ReadRest::push8(const std::uint8_t* input, int bits) {
    if (bits == 8) {
        push8(input);
        return;
    }
    bit_count_ += bits;
    for (int c = 0; c < channel_count_; ++c) {
        rest_[c] <<= bits;
        rest_[c] |= input[c] & ((1 << bits) - 1);
    }
}

void WAV::ReadRest::push8_msb(const std::uint8_t* bytes, int bits) {
    push8(bytes, bits);
}

void WAV::ReadRest::push8_lsb(const std::uint8_t* input, int bits) {
    uint8_t tmp[MAX_CHANNELS];
    for (int c = 0; c < channel_count_; ++c) {
        tmp[c] = SWAP_BITS_TABLE[input[c]];
    }
    push8(tmp, bits);
}