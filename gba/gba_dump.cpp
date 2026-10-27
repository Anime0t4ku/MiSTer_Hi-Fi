// Test tool (not part of the app): renders a song of a GBA ROM to raw 16-bit stereo PCM.
//   gba_dump <rom> list
//   gba_dump <rom> <song id> <seconds> <out.raw> [quality]
#include "gba_player.h"

#include <cmath>
#include <cstdio>
#include <cstdlib>
#include <string>
#include <vector>

int main(int argc, char **argv)
{
    if (argc < 3) { std::fprintf(stderr, "usage: gba_dump rom list | rom song seconds out [quality]\n"); return 2; }
    char err[256] = "";
    gba_rom *rom = gba_open(argv[1], err, sizeof err);
    if (!rom) { std::fprintf(stderr, "open: %s\n", err); return 1; }
    std::printf("%s (%s): %d songs listed\n", gba_title(rom), gba_code(rom), gba_song_count(rom));
    if (std::string(argv[2]) == "list") {
        for (int i = 0; i < gba_song_count(rom); i++) std::printf("%d ", gba_song_id(rom, i));
        std::printf("\n");
        return 0;
    }
    const int song = std::atoi(argv[2]), seconds = std::atoi(argv[3]);
    const int quality = argc > 5 ? std::atoi(argv[5]) : 1;
    const char *re = std::getenv("GBA_RATE");
    const int rate = re ? std::atoi(re) : 44100;
    gba_player *p = gba_start(rom, song, rate, 1, quality);
    if (!p) { std::fprintf(stderr, "start failed\n"); return 1; }
    std::FILE *f = std::fopen(argv[4], "wb");
    std::vector<float> buf(2 * 4410);
    std::vector<short> pcm(2 * 4410);
    long frames = 0; double peak = 0, sumsq = 0;
    while (frames < static_cast<long>(rate) * seconds) {
        int n = gba_render(p, buf.data(), 4410);
        for (int i = 0; i < 2 * n; i++) {
            double v = buf[i] > 1 ? 1 : buf[i] < -1 ? -1 : buf[i];
            peak = std::fabs(v) > peak ? std::fabs(v) : peak; sumsq += v * v;
            pcm[i] = static_cast<short>(v * 32767);
        }
        std::fwrite(pcm.data(), 2, 2 * n, f);
        frames += n;
        if (n < 4410) break;
    }
    std::fclose(f);
    std::printf("rendered %.1f s, peak %.3f, rms %.4f%s\n", frames / static_cast<double>(rate), peak, std::sqrt(sumsq / (2.0 * frames + 1)), frames < static_cast<long>(rate) * seconds ? " (song ended)" : "");
    gba_stop(p); gba_close(rom);
    return 0;
}
