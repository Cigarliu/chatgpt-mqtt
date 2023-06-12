# Distributed under the OSI-approved BSD 3-Clause License.  See accompanying
# file Copyright.txt or https://cmake.org/licensing for details.

cmake_minimum_required(VERSION 3.5)

file(MAKE_DIRECTORY
  "C:/Users/l1584/esp/esp-idf/components/bootloader/subproject"
  "D:/projects/chatgpt-mqtt/esp32_s3_box/build/bootloader"
  "D:/projects/chatgpt-mqtt/esp32_s3_box/build/bootloader-prefix"
  "D:/projects/chatgpt-mqtt/esp32_s3_box/build/bootloader-prefix/tmp"
  "D:/projects/chatgpt-mqtt/esp32_s3_box/build/bootloader-prefix/src/bootloader-stamp"
  "D:/projects/chatgpt-mqtt/esp32_s3_box/build/bootloader-prefix/src"
  "D:/projects/chatgpt-mqtt/esp32_s3_box/build/bootloader-prefix/src/bootloader-stamp"
)

set(configSubDirs )
foreach(subDir IN LISTS configSubDirs)
    file(MAKE_DIRECTORY "D:/projects/chatgpt-mqtt/esp32_s3_box/build/bootloader-prefix/src/bootloader-stamp/${subDir}")
endforeach()
if(cfgdir)
  file(MAKE_DIRECTORY "D:/projects/chatgpt-mqtt/esp32_s3_box/build/bootloader-prefix/src/bootloader-stamp${cfgdir}") # cfgdir has leading slash
endif()
