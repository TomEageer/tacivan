# -*- coding: utf-8 -*-
"""圆盘加张力：每片各自旋转、错位更大、上片"飞出去"。"""
exec(open('gen8.py').read().split("pills_kv('kv-day'")[0])

def discs_tense(name, t, angle=-14, w=560, h=132, ry=56, gap=-40, stagger=(-92,0,84), rots=(7,0,-5), widths=(0.84,0.94,1.0), lift=0, dx=0, dy=0):
    cx = 512
    total = 3*h + 2*gap
    top = 512 - total/2 + 20
    s = head(t)
    s.append(f'<g clip-path="url(#body)" transform="translate({512+dx} {512+dy}) scale(0.93) rotate({angle}) translate(-524 -512)">')
    s.append('<g filter="url(#sh)">')
    xs = [cx + d for d in stagger]
    def disc(i, c, alpha_top):
        y0 = top + i*(h+gap) - (lift if i==0 else 0); wi = w*widths[i]
        L, R = xs[i]-wi/2, xs[i]+wi/2
        g = f'<g transform="rotate({rots[i]} {xs[i]} {y0+h/2})">'
        d = f'M{L} {y0} A{wi/2} {ry} 0 0 0 {R} {y0} V{y0+h} A{wi/2} {ry} 0 0 1 {L} {y0+h} Z'
        return (g + f'<path d="{d}" fill="{c}" fill-opacity="{t["op"]}" style="mix-blend-mode:{t["blend"]}"/>'
                + f'<ellipse cx="{xs[i]}" cy="{y0}" rx="{wi/2}" ry="{ry}" fill="{c}" fill-opacity="{t["op"]}" style="mix-blend-mode:{t["blend"]}"/>'
                + f'<ellipse cx="{xs[i]}" cy="{y0}" rx="{wi/2-6}" ry="{ry-5}" fill="#FFFFFF" fill-opacity="{alpha_top*0.55:.2f}"/></g>')
    for i, c in reversed(list(enumerate(t['colors']))):
        s.append(disc(i, c, 0.40 if t is DAY else 0.28))
    s.append('</g>')
    for i, c in enumerate(t['colors']):
        y0 = top + i*(h+gap) - (lift if i==0 else 0); wi = w*widths[i]
        s.append(f'<g transform="rotate({rots[i]} {xs[i]} {y0+h/2})"><ellipse cx="{xs[i]}" cy="{y0-2}" rx="{wi/2-10}" ry="{ry-8}" fill="none" stroke="#FFFFFF" stroke-opacity="0.55" stroke-width="5"/></g>')
    s.append('</g></svg>')
    open(f'/tmp/icons/{name}.svg','w').write('\n'.join(s))

# T1：每片各转一点，错位拉到 ±90
discs_tense('t1-day', DAY, dx=2, dy=-46); discs_tense('t1-night', NIGHT, dx=2, dy=-44)
# T2：更狠——上片离群飞出、旋转更大、整体倾斜 18°
discs_tense('t2-day', DAY,   angle=-18, stagger=(-120,-10,96), rots=(12,2,-8), widths=(0.78,0.93,1.0), lift=26, dx=8, dy=-42)
discs_tense('t2-night', NIGHT, angle=-18, stagger=(-120,-10,96), rots=(12,2,-8), widths=(0.78,0.93,1.0), lift=26, dx=4, dy=-42)
print('ok')
