#include "custom_font.h"

#include <stdio.h>
#include <stdlib.h>

#define STB_TRUETYPE_IMPLEMENTATION
#include "stb_truetype.h"

static unsigned char *font_data;
static stbtt_fontinfo font_info;
static int font_ready;

static void clear_font(void) {
    free(font_data);
    font_data = NULL;
    font_ready = 0;
}

static unsigned char *read_file(const char *path, long *size_out) {
    FILE *f;
    long size;
    unsigned char *data;
    if (size_out) *size_out = 0;
    if (!path || !*path) return NULL;
    f = fopen(path, "rb");
    if (!f) return NULL;
    if (fseek(f, 0, SEEK_END) != 0) { fclose(f); return NULL; }
    size = ftell(f);
    if (size <= 0 || fseek(f, 0, SEEK_SET) != 0) { fclose(f); return NULL; }
    data = (unsigned char *)malloc((size_t)size);
    if (!data) { fclose(f); return NULL; }
    if (fread(data, 1, (size_t)size, f) != (size_t)size) {
        free(data);
        fclose(f);
        return NULL;
    }
    fclose(f);
    if (size_out) *size_out = size;
    return data;
}

int mh_font_probe(const char *path) {
    long size = 0;
    unsigned char *data = read_file(path, &size);
    stbtt_fontinfo info;
    int offset;
    (void)size;
    if (!data) return 0;
    offset = stbtt_GetFontOffsetForIndex(data, 0);
    if (offset < 0 || !stbtt_InitFont(&info, data, offset)) {
        free(data);
        return 0;
    }
    free(data);
    return 1;
}

int mh_font_init(const char *path) {
    long size = 0;
    unsigned char *data;
    int offset;
    (void)size;
    clear_font();
    data = read_file(path, &size);
    if (!data) return -1;
    offset = stbtt_GetFontOffsetForIndex(data, 0);
    if (offset < 0 || !stbtt_InitFont(&font_info, data, offset)) {
        free(data);
        return -1;
    }
    font_data = data;
    font_ready = 1;
    return 0;
}

void mh_font_shutdown(void) { clear_font(); }

int mh_font_has_glyph(int codepoint) {
    if (!font_ready) return 0;
    return stbtt_FindGlyphIndex(&font_info, codepoint) != 0;
}

int mh_font_advance(int codepoint, int pixel_height) {
    int advance = 0, bearing = 0;
    float scale;
    if (!font_ready || pixel_height <= 0) return 0;
    if (stbtt_FindGlyphIndex(&font_info, codepoint) == 0) return 0;
    scale = stbtt_ScaleForPixelHeight(&font_info, (float)pixel_height);
    stbtt_GetCodepointHMetrics(&font_info, codepoint, &advance, &bearing);
    (void)bearing;
    advance = (int)(advance * scale + 0.5f);
    return advance > 0 ? advance : pixel_height;
}

unsigned char *mh_font_render(int codepoint, int pixel_height, int *w, int *h, int *xoff, int *yoff, int *advance) {
    float scale;
    int raw_advance = 0, bearing = 0;
    if (w) *w = 0;
    if (h) *h = 0;
    if (xoff) *xoff = 0;
    if (yoff) *yoff = 0;
    if (advance) *advance = 0;
    if (!font_ready || pixel_height <= 0) return NULL;
    if (stbtt_FindGlyphIndex(&font_info, codepoint) == 0) return NULL;
    scale = stbtt_ScaleForPixelHeight(&font_info, (float)pixel_height);
    stbtt_GetCodepointHMetrics(&font_info, codepoint, &raw_advance, &bearing);
    (void)bearing;
    if (advance) {
        *advance = (int)(raw_advance * scale + 0.5f);
        if (*advance <= 0) *advance = pixel_height;
    }
    return stbtt_GetCodepointBitmap(&font_info, scale, scale, codepoint, w, h, xoff, yoff);
}

void mh_font_free_bitmap(unsigned char *bitmap) {
    if (bitmap) stbtt_FreeBitmap(bitmap, NULL);
}
