"""Tests for scripts/openwrt/uimage.py using synthetic images only."""
import importlib.util
import struct
import unittest
import zlib
from pathlib import Path

SPEC = importlib.util.spec_from_file_location(
    "uimage", Path(__file__).resolve().parents[1] / "scripts/openwrt/uimage.py")
uimage = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(uimage)


def fake_dtb(body):
    total = 8 + len(body)
    return uimage.FDT_MAGIC + struct.pack(">I", total) + body


def fake_zimage(code_len):
    head = bytearray(0x30)
    struct.pack_into("<III", head, 0x24, uimage.ZIMAGE_MAGIC, 0, 0x30 + code_len)
    return bytes(head) + b"\xaa" * code_len


def fake_uimage(payload, comp=0):
    info = {"time": 0, "load": 0x8000, "entry": 0x8000, "os": 5, "arch": 2,
            "type": 2, "comp": comp, "name": "ARM OpenWrt Linux-test"}
    return uimage.build(info, payload)


class UImage(unittest.TestCase):
    def setUp(self):
        self.zimage = fake_zimage(100)
        self.image = fake_uimage(self.zimage + fake_dtb(b"lm85"))

    def test_parse_roundtrip(self):
        info = uimage.parse(self.image)
        self.assertEqual(info["load"], 0x8000)
        self.assertEqual(info["name"], "ARM OpenWrt Linux-test")

    def test_swap_replaces_only_dtb_and_fixes_crcs(self):
        new = fake_dtb(b"adt7476-longer")
        out = uimage.swap_dtb(self.image, new)
        data = uimage.parse(out)["data"]  # parse() verifies both CRCs
        self.assertEqual(data, self.zimage + new)

    def test_rejects_corrupted_image(self):
        broken = bytearray(self.image)
        broken[-1] ^= 0xFF
        with self.assertRaises(uimage.ImageError):
            uimage.parse(bytes(broken))

    def test_rejects_incomplete_replacement_dtb(self):
        with self.assertRaises(uimage.ImageError):
            uimage.swap_dtb(self.image, fake_dtb(b"x")[:-1])

    def test_rejects_image_without_appended_dtb(self):
        with self.assertRaises(uimage.ImageError):
            uimage.swap_dtb(fake_uimage(self.zimage), fake_dtb(b"x"))

    def test_rejects_dtb_not_at_zimage_end(self):
        # A DTB at the payload end but past the recorded zImage end is ignored
        # by the decompressor, so swapping it would change nothing at boot.
        image = fake_uimage(self.zimage + b"pad" + fake_dtb(b"lm85"))
        with self.assertRaises(uimage.ImageError):
            uimage.swap_dtb(image, fake_dtb(b"x"))

    def test_rejects_compressed_payload(self):
        with self.assertRaises(uimage.ImageError):
            uimage.swap_dtb(fake_uimage(self.zimage + fake_dtb(b"a"), comp=1), fake_dtb(b"b"))

    def test_crc_is_standard_crc32(self):
        header = bytearray(self.image[:64])
        stored = struct.unpack(">I", header[4:8])[0]
        header[4:8] = b"\0\0\0\0"
        self.assertEqual(zlib.crc32(header), stored)


if __name__ == "__main__":
    unittest.main()
