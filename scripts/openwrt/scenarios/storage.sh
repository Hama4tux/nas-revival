# Storage scenario for sim_vm.py (one command per line, executed in the VM).
# "#reboot" = clean reboot, "#powercut" = QEMU killed without shutdown.
# Mirrors the NAS: one 3 TB disk as a single-member RAID1 md0 with ext4.
uci set network.lan.proto=dhcp; uci -q delete network.lan.ipaddr; uci -q delete network.lan.netmask; uci commit network; /etc/init.d/network restart; sleep 8
apk update >/dev/null && apk add mdadm kmod-md-raid1 e2fsprogs block-mount ksmbd-server kmod-fs-ksmbd 2>&1 | tail -1; /etc/init.d/mdadm enable; /etc/init.d/ksmbd enable; command -v mdadm >/dev/null && echo RESULT packages-installed
# Same as firmware/openwrt/files (nas-storage init and 90-nas-mdadm uci-defaults).
[ "$(uci -q get mdadm.@array[0].uuid)" = 52c5c44a:d2162820:f75d3464:799750f8 ] && uci delete mdadm.@array[0] && uci commit mdadm
printf '#!/bin/sh /etc/rc.common\nSTART=14\nUSE_PROCD=1\nstart_service() {\n\t/sbin/block mount\n}\n' > /etc/init.d/nas-storage; chmod +x /etc/init.d/nas-storage; /etc/init.d/nas-storage enable
mdadm --create /dev/md0 --run --level=1 --raid-devices=1 --force --metadata=1.2 /dev/vdb; mkfs.ext4 -q -F -m 0 -T largefile4 /dev/md0
UUID=$(mdadm --detail --scan | sed -n 's/.*UUID=\([^ ]*\).*/\1/p'); uci add mdadm array >/dev/null; uci set mdadm.@array[-1].uuid=$UUID; uci set mdadm.@array[-1].device=/dev/md0; uci commit mdadm
FSU=$(block info /dev/md0 | sed -n 's/.*UUID="\([^"]*\)".*/\1/p'); uci add fstab mount >/dev/null; uci set fstab.@mount[-1].uuid=$FSU; uci set fstab.@mount[-1].target=/mnt/data; uci set fstab.@mount[-1].enabled=1; uci set fstab.@global[0].check_fs=1; uci commit fstab
uci add ksmbd share >/dev/null; uci set ksmbd.@share[-1].name=data; uci set ksmbd.@share[-1].path=/mnt/data; uci set ksmbd.@share[-1].read_only=no; uci commit ksmbd; /etc/init.d/ksmbd enable
mkdir -p /mnt/data; block mount; dd if=/dev/urandom of=/mnt/data/test.bin bs=1M count=64 2>/dev/null; sha256sum /mnt/data/test.bin > /mnt/data/test.sha; sync; echo RESULT setup-done
#reboot
grep -q '^md0 : active' /proc/mdstat && echo RESULT md0-assembled; grep -q ' /mnt/data ' /proc/mounts && echo RESULT data-mounted; sha256sum -c /mnt/data/test.sha >/dev/null && echo RESULT data-intact; netstat -ltn | grep -q ':445 ' && echo RESULT smb-listening
free | sed -n 2p | sed 's/^/RESULT mem /'
dd if=/dev/urandom of=/mnt/data/unsynced.bin bs=1M count=32 2>/dev/null; echo RESULT unsynced-written
#powercut
grep -q ' /mnt/data ' /proc/mounts && echo RESULT data-mounted-after-powercut; sha256sum -c /mnt/data/test.sha >/dev/null && echo RESULT data-intact-after-powercut; ls -l /mnt/data/unsynced.bin 2>&1 | sed 's/^/RESULT unsynced: /'
