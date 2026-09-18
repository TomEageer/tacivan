# -*- coding: utf-8 -*-
"""B 的基础上做真正的近大远小：每一片整体等比缩放（宽、厚、椭圆都缩），不只是缩宽。"""
exec(open('gen8.py').read().split("pills_kv('kv-day'")[0])   # squircle / DAY / NIGHT / head
V1  = dict(DAY);   V1['colors']=('#F59E0B','#F0645A','#E0409A')
N1  = dict(NIGHT); N1['colors']=('#FBBF24','#FB7A6E','#F06BB5')

def discs_depth(name, t, angle=-14, w=560, h=132, ry=56, gap=-40, stagger=(-92,0,84), scales=(0.7,0.85,1.0), dx=2, dy=-46):
    cx = 512
    hs = [h*s for s in scales]
    total = sum(hs) + 2*gap
    top = 512 - total/2 + 20
    s = head(t)
    s.append(f'<g clip-path="url(#body)" transform="translate({512+dx} {512+dy}) scale(0.93) rotate({angle}) translate(-524 -512)">')
    s.append('<g filter="url(#sh)">')
    xs = [cx + d for d in stagger]
    ys = []
    y = top
    for hh in hs:
        ys.append(y); y += hh + gap
    def disc(i, c, alpha_top):
        sc = scales[i]; wi, hi, ri = w*sc, h*sc, ry*sc
        y0 = ys[i]; L, R = xs[i]-wi/2, xs[i]+wi/2
        d = f'M{L} {y0} A{wi/2} {ri} 0 0 0 {R} {y0} V{y0+hi} A{wi/2} {ri} 0 0 1 {L} {y0+hi} Z'
        return (f'<path d="{d}" fill="{c}" fill-opacity="{t["op"]}" style="mix-blend-mode:{t["blend"]}"/>'
                + f'<ellipse cx="{xs[i]}" cy="{y0}" rx="{wi/2}" ry="{ri}" fill="{c}" fill-opacity="{t["op"]}" style="mix-blend-mode:{t["blend"]}"/>'
                + f'<ellipse cx="{xs[i]}" cy="{y0}" rx="{wi/2-6*sc}" ry="{ri-5*sc}" fill="#FFFFFF" fill-opacity="{alpha_top}"/>')
    for i, c in reversed(list(enumerate(t['colors']))):
        s.append(disc(i, c, 0.40*0.55 if t is V1 else 0.28*0.55))
    s.append('</g>')
    for i, c in enumerate(t['colors']):
        sc = scales[i]; wi, ri = w*sc, ry*sc
        s.append(f'<ellipse cx="{xs[i]}" cy="{ys[i]-2}" rx="{wi/2-10*sc}" ry="{ri-8*sc}" fill="none" stroke="#FFFFFF" stroke-opacity="0.55" stroke-width="{5*sc:.1f}"/>')
    s.append('</g></svg>')
    open(f'/tmp/icons/{name}.svg','w').write('\n'.join(s))

discs_depth('D1-day', V1, scales=(0.72,0.86,1.0)); discs_depth('D1-night', N1, scales=(0.72,0.86,1.0), dy=-44)
discs_depth('D2-day', V1, scales=(0.60,0.80,1.0), stagger=(-110,-10,84)); discs_depth('D2-night', N1, scales=(0.60,0.80,1.0), stagger=(-110,-10,84), dy=-44)
print('ok')
