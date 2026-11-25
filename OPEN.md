# Otevřené body

## Stav (2026-11-25)
- Stav: Debian 13 (vlastní jádro 6.12.119) běží z vnitřního disku 1 TB (systém 30 GB, swap 1 GB, data 900 GB v /srv/data); jádro se načítá z USB flashky přes původní U-Boot ručně z konzole, NAND beze změny.
- Další krok: Samba na /srv/data, pak trvalý boot bez ruční obsluhy U-Bootu (vyžaduje rozhodnutí o zápisu prostředí U-Bootu).

## Čeká na rozhodnutí
- [ ] Zastavit odpočet U-Bootu (cca 3 s) a přečíst prostředí (printenv) jen pro čtení, žádný saveenv – podklad pro boot Debianu z USB. (2026-11-25)
- [ ] Před dočasným bootem OpenWrt (M3) zajistit vhodný USB–TTL adaptér (3,3 V) a ověřit pinout a napětí konkrétní desky; připojení a jakýkoli restart musí být samostatně schválené. (2026-11-16)

## Nedodělané
- [ ] Boot zatím jen ručně přes konzoli (scripts/debian/uboot-boot.py z notebooku s adaptérem); po výpadku proudu NAS nenaběhne do Debianu, ale do původního firmwaru z NAND. Trvalé řešení = změna bootcmd v prostředí U-Bootu (saveenv do NAND) – samostatně schválit. (2026-11-25)
- [ ] Dlouhý test SMART (smartctl -t long) nového disku ST1000LM014 zatím neproběhl; krátký test prošel, 0 přemapovaných sektorů. (2026-11-25)
- [ ] Starý kořen na USB flashce (oddíl rootfs) je zamrzlá kopie z 2026-11-25 jako záloha; neaktualizuje se. (2026-11-25)
- [ ] LED eSATA a COPY zatím nic neukazují a HDD bliká při činnosti kteréhokoli disku (i USB flashky); pro LED jednotlivých SATA portů je potřeba dohledat volbu jádra (per-port trigger ata1/ata2 v 6.12 chybí). Přiřazení tří LED síťového portu (link, aktivita, gigabit) ověřit pohledem. (2026-11-25)
- [ ] Původní U-Boot najde USB flashku jen při prvním `usb start` po zapnutí a jen v předním portu; výzva je `NSA310>>`, odpočet zastaví mezery/Ctrl+C posílané hned od banneru. (2026-11-25)
- [ ] Build rootfs (mmdebstrap) převzal resolv.conf stavebního počítače (127.0.0.53 bez resolved) – v receptu doinstalovat systemd-resolved nebo zapsat správný resolv.conf; na NASu opraveno ručně. (2026-11-25)
- [ ] AGENTS.md (sekce Tento projekt) stále zakazuje přístup k živému NASu a popisuje fázi OpenWrt; po přechodu na Debian ho přepsat podle skutečného postupu. (2026-11-25)
- [ ] Debian 13 armel nemá oficiální jádro Kirkwood: jádro (mainline LTS 6.12) aktualizujeme sami – zautomatizovat build a hlídat nové verze 6.12.x. (2026-11-25)
- [ ] Vlastník zvolil Debian 13 místo OpenWrt (2026-11-25): přepsat ADR 0001 a roadmapu (kořen na USB/disku, komunitní jádro Kirkwood, konec armel po Debianu 13) a nejdřív Debian vyzkoušet z USB přes původní U-Boot bez saveenv a bez zápisu do NAND. (2026-11-25)
- [ ] NAS se po výpadku napájení nezapne sám (nutné tlačítko) – zohlednit u napájení z Delty a v plánu obnovy. (2026-11-18)
- [ ] Instalace OpenWrt do NAND vyžaduje samostatný plán: oddíl ubi v OpenWrt (od 0x140000) přepíše prostředí U-Bootu i všechny oddíly výrobce a uboot_env v OpenWrt (0xC0000) leží uvnitř oddílu původního U-Bootu; buď nový U-Boot z OpenWrt, nebo vlastní mapa oddílů – rozhodnout před M7. (2026-11-18)
- [ ] Při dočasném bootu (M3) nastavit bootargs jen v RAM (setenv bez saveenv): jádro OpenWrt přebírá příkazový řádek od původního U-Bootu (root=/dev/nfs, mtdparts=nand_mtd); podle analýzy je neškodný, na hardwaru neověřeno. Pod OpenWrt s původním U-Bootem nepoužívat fw_setenv. (2026-11-18)
- [ ] Do návrhu backendu: nepovolit sdílení na nepřipojené cestě (zápis by šel do NAND), po změně konfigurace volat sync před hlášením úspěchu, pole a disky adresovat podle UUID – zjištěno v simulaci. (2026-11-18)
- [ ] Při bootu OpenWrt (M3) ověřit, že se modul lm85 naváže na čip na i2c 0x2e a ventilátor reaguje na teplotu; ve stock firmware je LM85 potvrzený. (2026-11-19)
- [ ] Telnet je nešifrovaný (naběhne sám po zapnutí přes háček na md0, ověřeno 2026-11-19); v OpenWrt ho nahradit SSH s klíčem. (2026-11-16)
- [ ] Doověřit PCB revizi desky (procesor, takt, síťový čip a čip ventilátoru už potvrdila diagnostika 2026-11-19). (2026-11-16)
- [ ] Zjistit skutečné USB/FAT32 chování bootloaderu, podporu kernelu a DT/ATAG handoff a návrat při selhání; statické záznamy příkazů nejsou funkční test. (2026-11-16)
- [ ] Doplnit původní nástroj a volby zálohování, ECC/OOB a bad-block politiku a kvalifikovat obnovu; devět ověřených hashů a oddělení OOB nepotvrzují obnovitelnost a analýza ECC neopravovala. (2026-11-16)
- [ ] Dokončit offline posouzení původní dokumentace výrobce a komunitních návodů; přímý archiv výrobce a Doozan nebyly dostupné, historické návody nejsou schválený postup. (2026-11-16)
- [ ] Před přijetím ADR ověřit bezpečnostní životní cyklus vybrané větve OpenWrt, potřebné balíčky a rozdíly NSA310 proti profilu NSA310b. (2026-11-16)
- [ ] Změřit rozpočet RAM, velikosti systému a výkonu TLS/SMB/SFTP i možnost bezpečného chlazení; uvedené limity jsou cíle, nikoliv výsledky. (2026-11-16)
- [ ] Upřesnit podepisování aktualizací, důvěryhodné klíče, migrace konfigurace a recovery při výpadku napájení bez předpokladu dvou NAND slotů; mechanismus dosud není implementovaný. (2026-11-16)
- [ ] Začlenit tools/hamaproject.patch do přehledu a ověřit skutečně vydaný portfolio.json po standardním nasazení; dřívější typecheck a místní sběr prošly, 10 HTTP testů blokoval listen EPERM a HamaProject je mimo povolený zápis relace, body jsou zatím pouze lokální; podklad Stage 1 je aktualizován lokálně, skutečné nasazení a ověření webu zůstává nedokončené. (2026-11-16)

## Dokončené etapy
- Systém přesunut na nový vnitřní disk 1 TB (30 GB systém, 1 GB swap, zbytek data), boot s kořenem na disku ověřen; vadný 3TB disk vyřazen. (2026-11-25)
- Debian na NASu s jádrem s LED triggery: ověřený boot z USB přes konzoli, tovární MAC, firmware síťovky, vnitřní disk viditelný, LED nastavuje služba nsa310-leds. (2026-11-25)
- Debian na NASu: opravené DNS (systemd-resolved), firmware-realtek, nové heslo roota (v secret.env), LED SYS svítí. (2026-11-25)
- První boot Debianu 13 (jádro 6.12.119, nsa310a DTB) z USB na NASu, přihlášení root na konzoli. (2026-11-25)
- Sestaven Debian 13.8 armel (mmdebstrap) a jádro 6.12.119 pro NSA310a, nahráno na USB flashku. (2026-11-25)
- Obousměrná sériová konzole: TX NASu na J2 pin 2, TXD adaptéru zapojen; původní firmware na konzoli dává root shell BusyBox bez hesla. (2026-11-25)
- Sériová konzole funguje: CP2102 na J2, 115200 8N1; U-Boot 1.1.4 čte jádro z NAND 0xC80000 (kernel_1, 0xA00000) do RAM 0x2000000, legacy uImage; rozpor kernel_addr vyřešen. Výpis mimo git v ~/VScode/NAS310. (2026-11-25)
- Čtecí diagnostika na NASu: 88F6281 A1, LM85, RTL8111D sedí na OpenWrt NSA310b; původní jádro bez kexec, Telnet po zapnutí naběhne sám. (2026-11-19)
- Ověřený build OpenWrt 25.12.6 (podpis a SHA-256) a simulace v QEMU: ARMv5TE userspace bez chyb, plný systém v 256 MB s RAID1/ext4/SMB, reboot a výpadek napájení; opraveno pořadí mdadm/fstab a ukázkové pole v konfiguraci mdadm. (2026-11-18)
- Stage 1 vyhodnocen a zastaven: build uClibc-ng 1.0.59 narazil na glibc podmínku v linux/socket.h z 2.6.31.8; rozbité buildy a cache smazány, další směr OpenWrt. (2026-11-16)
- Po opakovaných pádech VM (příčina na straně Proxmoxu vyřešena) obnoven vynulovaný checkout Buildrootu a poškozený git repozitáře, smazány rozbité buildy a cache. (2026-11-16)
- Opraven Stage 1 build, který zamrazil VM (linux.git klonovaný do tmpfs /tmp): hlavičky z kernel.org tarballu s hashem, zákaz tmpfs cest a -j podle volné RAM. (2026-11-16)
- Založen dokumentační bootstrap, ADR, návrh HAL, bezpečnostní model a plán obnovy bez zásahu do zařízení. (2026-11-16)
- Dokončena lokální analýza OOB struktury a U-Bootu, zapsán root Telnet a ověřené hardwarové údaje a posouzeny možnosti bootu bez UART; originální mtd0 nezměněn. (2026-11-16)
- Připraven Stage 1 build postup a ruční chroot/SSH controller; 14 bezpečnostních a ABI kontrol prošlo lokálně, úplný build a NAS testy zůstávají neověřené. (2026-11-16)
