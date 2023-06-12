/*
 * SPDX-FileCopyrightText: 2022-2023 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: CC0-1.0
 */

#include "lvgl.h"
#include "audio_player.h"
#include "file_iterator.h"
#include "esp_err.h"
#include "esp_log.h"
#include "bsp_board.h"

static const char *TAG = "ui_audio";

static file_iterator_instance_t *file_iterator;
static uint8_t g_sys_volume;


uint8_t get_sys_volume()
{
    return g_sys_volume;
}

void ui_audio_start(file_iterator_instance_t *i)
{
     static lv_style_t style;
     lv_style_init(&style);
     lv_style_set_radius(&style, 5);

     /*Make a gradient*/
     lv_style_set_width(&style, 150);
     lv_style_set_height(&style, LV_SIZE_CONTENT);

     lv_style_set_pad_ver(&style, 20);
     lv_style_set_pad_left(&style, 5);

     lv_style_set_x(&style, lv_pct(50));
     lv_style_set_y(&style, 80);

     /*Create an object with the new style*/
     lv_obj_t * obj = lv_obj_create(lv_scr_act());
     lv_obj_add_style(obj, &style, 0);

     lv_obj_t * label = lv_label_create(obj);
     lv_label_set_text(label, "Hello");
}
