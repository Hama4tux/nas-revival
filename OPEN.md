# Otevřené body

## Stav (2026-11-18)
- Stav: Na pracovní stanici je ověřený build OpenWrt 25.12.6 pro NSA310 a třívrstvá simulace v QEMU (statická analýza image, userspace na ARMv5TE, celý systém s 256 MB RAM a 3TB diskem). Našly se a opravily dvě chyby v připojování RAIDu. Na NASu se nic neměnilo.
- Další krok: zapnout NAS tlačítkem, ověřit Telnet a spustit čtecí diagnostiku přes Telnet (rozhodne LM85 nebo ADT7476 a tím, kterou initramfs variantu použít), pořídit USB–TTL adaptér a pak dočasný boot z RAM (M3).

## Čeká na rozhodnutí
- [ ] Schválit a spustit čtecí diagnostiku scripts/openwrt/probe-readonly.sh přes root Telnet (výstup uložit do output/, necommitovat); bez ní nelze ověřit, že profil OpenWrt NSA310b sedí na A203 (SoC, ventilátor ADT7476/LM85, síť r8169, U-Boot prostředí). (2026-11-16)
- [ ] Před dočasným bootem OpenWrt (M3) zajistit vhodný USB–TTL adaptér (3,3 V) a ověřit pinout a napětí konkrétní desky; připojení a jakýkoli restart musí být samostatně schválené. (2026-11-16)

## Nedodělané
- [ ] Čtecí diagnostika je schválená (2026-11-18), ale neproběhla: NAS po výpadku 12V větve na Deltě 3 nenaběhl sám a je třeba ho zapnout tlačítkem. Pak spustit `python3 scripts/openwrt/telnet_run.py <IP NASu> ../secret.env scripts/openwrt/probe-readonly.sh > output/probe.txt`. (2026-11-18)
- [ ] Po zapnutí ověřit, jestli Telnet naběhne sám přes háček telnetd_boot na md0; když ne, otevřít ho jako dřív ve webovém rozhraní přes nas_proxy.py po přihlášení adresou `/r41773,/adv,/cgi-bin/remote_help-cgi?type=backdoor` (servisní přístup ZyXEL). (2026-11-18)
- [ ] NAS se po výpadku napájení nezapne sám (nutné tlačítko) – zohlednit u napájení z Delty a v plánu obnovy. (2026-11-18)
- [ ] Instalace OpenWrt do NAND vyžaduje samostatný plán: oddíl ubi v OpenWrt (od 0x140000) přepíše prostředí U-Bootu i všechny oddíly výrobce a uboot_env v OpenWrt (0xC0000) leží uvnitř oddílu původního U-Bootu; buď nový U-Boot z OpenWrt, nebo vlastní mapa oddílů – rozhodnout před M7. (2026-11-18)
- [ ] Při dočasném bootu (M3) nastavit bootargs jen v RAM (setenv bez saveenv): jádro OpenWrt přebírá příkazový řádek od původního U-Bootu (root=/dev/nfs, mtdparts=nand_mtd); podle analýzy je neškodný, na hardwaru neověřeno. Pod OpenWrt s původním U-Bootem nepoužívat fw_setenv. (2026-11-18)
- [ ] Do návrhu backendu: nepovolit sdílení na nepřipojené cestě (zápis by šel do NAND), po změně konfigurace volat sync před hlášením úspěchu, pole a disky adresovat podle UUID – zjištěno v simulaci. (2026-11-18)
- [ ] Ověřit na hardwaru chování ventilátoru bez řídicího ovladače (varianta ADT7476 initramfs neobsahuje modul adt7475, otáčky zůstanou podle výchozího stavu čipu). (2026-11-18)
- [ ] Po příštím vypnutí/zapnutí NASu ověřit, že Telnet naběhne sám: boot hook `telnetd_boot` je na disku md0 (zy-pkgs init.d + USRPKG_DEPS_START), spouští ho zypkg_controller.sh ze rcS2; bez disku md0 nenaběhne. Telnet je nešifrovaný, cílově nahradit SSH s klíčem v OpenWrt. (2026-11-16)
- [ ] Ověřit zdraví disku pod /dev/md0 (SMART, vadné sektory); vlastník potvrdil, že svazek je prázdný po formátu a lze ho volně použít, zdraví disku tím ale ověřené není. (2026-11-16)
- [ ] Doověřit PCB revizi, konkrétní SoC a jeho takt, síťový čip a řadič ventilátoru; A203 a CPU jádro Feroceon samy neurčují správný profil. (2026-11-16)
- [ ] Vysvětlit rozpor kernel_addr=480000 v exportu prostředí proti začátku kernel_1 na 0xC80000 podle uloženého mtdparts; žádnou hodnotu bez ověření nepoužívat k bootování ani zápisu. (2026-11-16)
- [ ] Zjistit skutečné USB/FAT32 chování bootloaderu, podporu kernelu a DT/ATAG handoff a návrat při selhání; statické záznamy příkazů nejsou funkční test. (2026-11-16)
- [ ] Pokud se bude zvažovat kexec, získat důkaz CONFIG_KEXEC a konkrétní implementace běžícího kernelu; verze kernelu ani root shell jeho použitelnost nepotvrzují. (2026-11-16)
- [ ] Doplnit původní nástroj a volby zálohování, ECC/OOB a bad-block politiku a kvalifikovat obnovu; devět ověřených hashů a oddělení OOB nepotvrzují obnovitelnost a analýza ECC neopravovala. (2026-11-16)
- [ ] Dokončit offline posouzení původní dokumentace výrobce a komunitních návodů; přímý archiv výrobce a Doozan nebyly dostupné, historické návody nejsou schválený postup. (2026-11-16)
- [ ] Před přijetím ADR ověřit bezpečnostní životní cyklus vybrané větve OpenWrt, potřebné balíčky a rozdíly NSA310 proti profilu NSA310b. (2026-11-16)
- [ ] Změřit rozpočet RAM, velikosti systému a výkonu TLS/SMB/SFTP i možnost bezpečného chlazení; uvedené limity jsou cíle, nikoliv výsledky. (2026-11-16)
- [ ] Upřesnit podepisování aktualizací, důvěryhodné klíče, migrace konfigurace a recovery při výpadku napájení bez předpokladu dvou NAND slotů; mechanismus dosud není implementovaný. (2026-11-16)
- [ ] Začlenit tools/hamaproject.patch do přehledu a ověřit skutečně vydaný portfolio.json po standardním nasazení; dřívější typecheck a místní sběr prošly, 10 HTTP testů blokoval listen EPERM a HamaProject je mimo povolený zápis relace, body jsou zatím pouze lokální; podklad Stage 1 je aktualizován lokálně, skutečné nasazení a ověření webu zůstává nedokončené. (2026-11-16)

## Dokončené etapy
- Ověřený build OpenWrt 25.12.6 (podpis a SHA-256) a simulace v QEMU: ARMv5TE userspace bez chyb, plný systém v 256 MB s RAID1/ext4/SMB, reboot a výpadek napájení; opraveno pořadí mdadm/fstab a ukázkové pole v konfiguraci mdadm. (2026-11-18)
- Stage 1 vyhodnocen a zastaven: build uClibc-ng 1.0.59 narazil na glibc podmínku v linux/socket.h z 2.6.31.8; rozbité buildy a cache smazány, další směr OpenWrt. (2026-11-16)
- Po opakovaných pádech VM (příčina na straně Proxmoxu vyřešena) obnoven vynulovaný checkout Buildrootu a poškozený git repozitáře, smazány rozbité buildy a cache. (2026-11-16)
- Opraven Stage 1 build, který zamrazil VM (linux.git klonovaný do tmpfs /tmp): hlavičky z kernel.org tarballu s hashem, zákaz tmpfs cest a -j podle volné RAM. (2026-11-16)
- Založen dokumentační bootstrap, ADR, návrh HAL, bezpečnostní model a plán obnovy bez zásahu do zařízení. (2026-11-16)
- Dokončena lokální analýza OOB struktury a U-Bootu, zapsán root Telnet a ověřené hardwarové údaje a posouzeny možnosti bootu bez UART; originální mtd0 nezměněn. (2026-11-16)
- Připraven Stage 1 build postup a ruční chroot/SSH controller; 14 bezpečnostních a ABI kontrol prošlo lokálně, úplný build a NAS testy zůstávají neověřené. (2026-11-16)
