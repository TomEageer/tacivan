# -*- coding: utf-8 -*-
"""玻璃圆盘，按叠层片的方式放松：整体倾斜、逐片错位、交叠更深。"""
import math
exec(open('gen8.py').read().split("pills_kv('kv-day'")[0])   # 复用 squircle/DAY/NIGHT/head

def discs_loose(name, t, angle=-10, w=540, h=138, ry=54, gap=-34, stagger=48, widths=(1.0,1.0,1.0)):
    cx = 512
    total = 3*h + 2*gap
    top = 512 - total/2 + 16
    s = head(t)
    s.append(f'<g clip-path="url(#body)" transform="rotate({angle} 512 512)">')
    s.append('<g filter="url(#sh)">')
    # 每片各自的中心 x：上片左移、下片右移，和之前的片一样有"抽出来"的动势
    xs = [cx - stagger, cx, cx + stagger]
    for i, c in reversed(list(enumerate(t['colors']))):
        y0 = top + i*(h+gap); wi = w*widths[i]
        L, R = xs[i]-wi/2, xs[i]+wi/2
        d = f'M{L} {y0} A{wi/2} {ry} 0 0 0 {R} {y0} V{y0+h} A{wi/2} {ry} 0 0 1 {L} {y0+h} Z'
        s.append(f'<path d="{d}" fill="{c}" fill-opacity="{t["op"]}" style="mix-blend-mode:{t["blend"]}"/>')
        s.append(f'<ellipse cx="{xs[i]}" cy="{y0}" rx="{wi/2}" ry="{ry}" fill="{c}" fill-opacity="{t["op"]}" style="mix-blend-mode:{t["blend"]}"/>')
        s.append(f'<ellipse cx="{xs[i]}" cy="{y0}" rx="{wi/2-6}" ry="{ry-5}" fill="#FFFFFF" fill-opacity="{0.40 if t is DAY else 0.28}"/>')
    s.append('</g>')
    for i, c in enumerate(t['colors']):
        y0 = top + i*(h+gap); wi = w*widths[i]
        s.append(f'<ellipse cx="{xs[i]}" cy="{y0-2}" rx="{wi/2-10}" ry="{ry-8}" fill="none" stroke="#FFFFFF" stroke-opacity="0.55" stroke-width="5"/>')
    s.append('</g></svg>')
    open(f'/tmp/icons/{name}.svg','w').write('\n'.join(s))

discs_loose('dl-a-day', DAY);   discs_loose('dl-a-night', NIGHT)
discs_loose('dl-b-day', DAY,   widths=(0.86,0.95,1.0), stagger=60)
discs_loose('dl-b-night', NIGHT, widths=(0.86,0.95,1.0), stagger=60)
print('ok')
