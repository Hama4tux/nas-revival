#!/usr/bin/env python3
"""Inspect a legacy U-Boot uImage and swap the device tree appended to its zImage.

The stock NSA310 U-Boot 1.1.4 cannot pass a device tree, so OpenWrt kirkwood
images carry the DTB appended to the zImage (CONFIG_ARM_APPENDED_DTB). Replacing
that DTB lets the same kernel describe a different board variant, e.g. the
original NSA310 fan controller ADT7476 instead of the NSA310b LM85.

  uimage.py info IMAGE
  uimage.py extract-dtb IMAGE OUT.dtb
  uimage.py swap-dtb IMAGE NEW.dtb OUT
"""
import struct
import sys
import time
import zlib

UIMAGE_MAGIC = 0x27051956
FDT_MAGIC = b"\xd0\x0d\xfe\xed"
ZIMAGE_MAGIC = 0x016F2818  # little-endian word at offset 0x24 of an ARM zImage
HEADER = struct.Struct(">IIIIIIIBBBB32s")  # 64 bytes, see U-Boot include/image.h


class ImageError(ValueError):
    pass


def parse(blob):
    if len(blob) < HEADER.size:
        raise ImageError("file shorter than a uImage header")
    (magic, hcrc, stamp, size, load, entry, dcrc,
     os_, arch, kind, comp, name) = HEADER.unpack_from(blob)
    if magic != UIMAGE_MAGIC:
        raise ImageError("not a legacy uImage (bad magic)")
    header = bytearray(blob[:HEADER.size])
    header[4:8] = b"\0\0\0\0"
    if zlib.crc32(header) != hcrc:
        raise ImageError("header CRC mismatch")
    data = blob[HEADER.size:HEADER.size + size]
    if len(data) != size or zlib.crc32(data) != dcrc:
        raise ImageError("data size or CRC mismatch")
    return {"time": stamp, "load": load, "entry": entry, "os": os_, "arch": arch,
            "type": kind, "comp": comp, "name": name.rstrip(b"\0").decode(errors="replace"),
            "data": data}


def appended_dtb_offset(data):
    """Offset of the DTB that ends exactly at the end of the payload."""
    pos = data.rfind(FDT_MAGIC)
    while pos != -1:
        total = struct.unpack_from(">I", data, pos + 4)[0] if pos + 8 <= len(data) else 0
        if pos + total == len(data):
            return pos
        pos = data.rfind(FDT_MAGIC, 0, pos)
    raise ImageError("no device tree appended at the end of the kernel")


def build(info, data):
    fields = [UIMAGE_MAGIC, 0, info["time"], len(data), info["load"], info["entry"],
              zlib.crc32(data), info["os"], info["arch"], info["type"], info["comp"],
              info["name"].encode()[:32]]
    header = bytearray(HEADER.pack(*fields))
    header[4:8] = struct.pack(">I", zlib.crc32(header))
    return bytes(header) + data


def swap_dtb(blob, dtb):
    if not dtb.startswith(FDT_MAGIC) or struct.unpack_from(">I", dtb, 4)[0] != len(dtb):
        raise ImageError("replacement is not a complete flattened device tree")
    info = parse(blob)
    if info["comp"] != 0:
        raise ImageError("compressed uImage payload; appended DTB not reachable")
    data = info["data"]
    offset = appended_dtb_offset(data)
    # The decompressor looks for the DTB at the zImage end recorded in its own
    # header; a DTB anywhere else would be silently ignored at boot.
    magic, _, zimage_end = struct.unpack_from("<III", data, 0x24)
    if magic != ZIMAGE_MAGIC or zimage_end != offset:
        raise ImageError("DTB does not start at the zImage end; not an appended DTB")
    return build(info, data[:offset] + dtb)


def main(argv):
    if len(argv) >= 2 and argv[0] == "info":
        info = parse(open(argv[1], "rb").read())
        data = info.pop("data")
        info["size"] = len(data)
        info["dtb_offset"] = appended_dtb_offset(data)
        info["dtb_size"] = len(data) - info["dtb_offset"]
        info["time"] = time.strftime("%Y-%m-%d %H:%M:%S", time.gmtime(info["time"]))
        for key in ("load", "entry"):
            info[key] = hex(info[key])
        for key, value in info.items():
            print(f"{key}: {value}")
    elif len(argv) == 3 and argv[0] == "extract-dtb":
        data = parse(open(argv[1], "rb").read())["data"]
        open(argv[2], "wb").write(data[appended_dtb_offset(data):])
    elif len(argv) == 4 and argv[0] == "swap-dtb":
        out = swap_dtb(open(argv[1], "rb").read(), open(argv[2], "rb").read())
        open(argv[3], "wb").write(out)
    else:
        print(__doc__, file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main(sys.argv[1:]))
    except ImageError as err:
        print(f"error: {err}", file=sys.stderr)
        sys.exit(1)
