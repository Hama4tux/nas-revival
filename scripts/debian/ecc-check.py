#!/usr/bin/env python3
"""Check that OOB ECC bytes in a raw NAND dump (nanddump -n -o, 2048+64 B pages) match
Linux soft Hamming (non-SMC order, eccpos 40-63) as written by the stock U-Boot 1-bit ECC.
Usage: ecc-check.py dump.raw   ("swap" = Linux non-SMC order)."""
import sys
def ham(d):
    # classic 256-byte hamming (SmartMedia), returns (lp0..lp15 bits, cp0..cp5)
    lp=[0]*16; cp=[0]*6
    for i,b in enumerate(d):
        p=bin(b).count('1')&1
        for k in range(8):
            if p: lp[2*k + ((i>>k)&1)] ^= 1
    col=0
    for b in d: col^=b
    bit=lambda n:(col>>n)&1
    cp[0]=bit(0)^bit(2)^bit(4)^bit(6); cp[1]=bit(1)^bit(3)^bit(5)^bit(7)
    cp[2]=bit(0)^bit(1)^bit(4)^bit(5); cp[3]=bit(2)^bit(3)^bit(6)^bit(7)
    cp[4]=bit(0)^bit(1)^bit(2)^bit(3); cp[5]=bit(4)^bit(5)^bit(6)^bit(7)
    b0=sum(lp[j]<<j for j in range(8)); b1=sum(lp[8+j]<<j for j in range(8))
    b2=sum(cp[j]<<(j+2) for j in range(6))
    return (~b0&255, ~b1&255, (~b2&255)|3)
raw=open(sys.argv[1],'rb').read()
res={'smc':0,'swap':0,'none':0,'blank':0}; pages=len(raw)//2112
for p in range(pages):
    pg=raw[p*2112:(p+1)*2112]; data,oob=pg[:2048],pg[2048:]
    if data==b'\xff'*2048: res['blank']+=1; continue
    ok_s=ok_w=True
    for s in range(8):
        a,b,c=ham(data[s*256:(s+1)*256]); st=oob[40+3*s:43+3*s]
        ok_s &= bytes([a,b,c])==st; ok_w &= bytes([b,a,c])==st
    res['smc' if ok_s else 'swap' if ok_w else 'none']+=1
print(pages,'pages',res); print('oob page0',raw[2048:2112].hex())
