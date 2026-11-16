# Changelog

## 2026-11-16
- Doplněna vlastníkem ověřená konfigurace NSA310 A203, přístup root přes Telnet a evidence devíti záloh NAND s OOB mimo repozitář. Lokální analýza mtd0 potvrdila prokládání 2048+64 bajtů, integritu obrazu a U-Boot 1.1.4 / Marvell 3.4.19; původní dump zůstal beze změny.
- Zapsány kompilované USB/FAT/ext2/bootm/TFTP příkazy, neověřený kexec a nesoulad uložené adresy kernelu s MTD mapou; bezpečný jednorázový boot bez UART není prokázán. Doplněno ignorování citlivých záloh, pracovních binárek a secret.env; bez připojení k NASu či změny bootování.
- Založen lokální projekt NAS Revival s vizí, architekturou, porovnáním OpenWrt/Debian ARMEL/Buildroot, návrhem úsporného backendu a hardwarové abstrakce.
- Doplněny zdroje a nejistoty variant NSA310, bezpečnostní model, návrh obnovy, milníky a první experiment bez přístupu k zařízení. Nevznikl firmware ani změny na NASu.
- Připraven lokální podklad pro přehled projektů; jeho začlenění a nasazení zůstává závislé na přístupu mimo pracovní sandbox.
