# Changelog

## 2026-11-18
- Přidán skript pro spuštění čtecí diagnostiky přes Telnet NASu (přihlašovací údaje bere ze secret.env a nikam je nevypisuje). Diagnostika zatím neproběhla, protože NAS po výpadku napájení nenaběhl.
- Build OpenWrt 25.12.6 pro NSA310 se dá zopakovat jedním skriptem: stažení s ověřením podpisu OpenWrt, Image Builder s balíčky pro NAS (RAID, SMART, SMB, SFTP, ventilátor ADT7476) a vlastními opravami.
- Simulace na pracovní stanici ve třech vrstvách: rozbor image (device tree, jádro, mapa NAND), programy pro NAS na emulovaném procesoru ARMv5TE a celý OpenWrt v QEMU s 256 MB RAM a 3TB diskem včetně restartu a vytržení napájení.
- Opraveny dvě chyby, které simulace odhalila: datový RAID se po startu nepřipojil (mdadm startoval až po fstab) a ukázkový záznam v konfiguraci mdadm bránil sestavení pole.
- Připravena varianta dočasného bootovacího image pro původní NSA310 s čipem ventilátoru ADT7476.
- Zjištěno, že rozložení NAND v OpenWrt se překrývá s původním U-Bootem a přepíše oddíly výrobce; instalace do NAND potřebuje samostatný plán.

## 2026-11-16
- Stage 1 zastaven: první úplný build spadl v uClibc-ng 1.0.59 na kolizi se starým linux/socket.h z 2.6.31.8. Protože OpenWrt přinese vlastní kernel i userspace a čtecí přístup už dává root Telnet, projekt přechází rovnou na kvalifikaci OpenWrt. Recept Stage 1 zůstává v repozitáři jen pro referenci.
- Roadmapa nově vysvětluje rozdělení rolí: OpenWrt zůstává cílovým systémem, Buildroot slouží jen k sestavení malého chrootu na původním kernelu. Zapsáno rozhodnutí vlastníka, že zařízení je bez praktické hodnoty a riziko experimentu přijímá; záchrana přes UART a uložené dumpy NAND.
- Připraven Stage 1: konfigurace udržovaného Buildroot/uClibc-ng pro ARMv5 soft-float a hlavičky 2.6.31.8, minimální chroot bez initu, offline kontrola ELF a závislostí a ruční instalace/start/stop Dropbearu na 2222 pouze s veřejným klíčem. Host klíče vzniknou až na cíli; původní služby ani síť skripty nekonfigurují.
- Doplněny lokální testy odmítnutí chybného ABI, obsazeného portu a cizích mountů i úklidu při selhání soukromých PTY. Úplný build/rootfs a skutečná kompatibilita s kernelem nejsou ověřené kvůli nedostupným zdrojovým downloadům; nic nebylo spuštěno na NASu.
- Doplněna vlastníkem ověřená konfigurace NSA310 A203, přístup root přes Telnet a evidence devíti záloh NAND s OOB mimo repozitář. Lokální analýza mtd0 potvrdila prokládání 2048+64 bajtů, integritu obrazu a U-Boot 1.1.4 / Marvell 3.4.19; původní dump zůstal beze změny.
- Zapsány kompilované USB/FAT/ext2/bootm/TFTP příkazy, neověřený kexec a nesoulad uložené adresy kernelu s MTD mapou; bezpečný jednorázový boot bez UART není prokázán. Doplněno ignorování citlivých záloh, pracovních binárek a secret.env; bez připojení k NASu či změny bootování.
- Založen lokální projekt NAS Revival s vizí, architekturou, porovnáním OpenWrt/Debian ARMEL/Buildroot, návrhem úsporného backendu a hardwarové abstrakce.
- Doplněny zdroje a nejistoty variant NSA310, bezpečnostní model, návrh obnovy, milníky a první experiment bez přístupu k zařízení. Nevznikl firmware ani změny na NASu.
- Připraven lokální podklad pro přehled projektů; jeho začlenění a nasazení zůstává závislé na přístupu mimo pracovní sandbox.
