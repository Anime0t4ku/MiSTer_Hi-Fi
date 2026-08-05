#ifndef MISTERHIFI_CUSTOM_FONT_H
#define MISTERHIFI_CUSTOM_FONT_H

int mh_font_probe(const char *path);
int mh_font_init(const char *path);
void mh_font_shutdown(void);
int mh_font_has_glyph(int codepoint);
int mh_font_advance(int codepoint, int pixel_height);
unsigned char *mh_font_render(int codepoint, int pixel_height, int *w, int *h, int *xoff, int *yoff, int *advance);
void mh_font_free_bitmap(unsigned char *bitmap);

#endif
