# -*- coding: utf-8 -*-
"""暖色叠层，加数据库元素，日/夜两套。"""
import math
CANVAS, BODY, N = 1024, 824, 5.0
def squircle(cx, cy, r, n=N, steps=900):
    pts=[]
    for i in range(steps):
        th=2*math.pi*i/steps; ct,st=math.cos(th),math.sin(th)
        pts.append((cx+r*math.copysign(abs(ct)**(2/n),ct), cy+r*math.copysign(abs(st)**(2/n),st)))
    return 'M %.2f %.2f '%pts[0]+' '.join('L %.2f %.2f'%p for p in pts[1:])+' Z'
P = squircle(512, 512, BODY/2)

DAY  = dict(body='<stop offset="0" stop-color="#FFFFFF"/><stop offset="1" stop-color="#EEF1F7"/>', vign='#0B1220', vop=0.10,
            colors=('#F59E0B','#EC4899','#6366F1'), blend='multiply', op=0.86, shadow=0.20, rim=0.75)
NIGHT= dict(body='<stop offset="0" stop-color="#1A1F2E"/><stop offset="1" stop-color="#0A0D17"/>', vign='#000000', vop=0.40,
            colors=('#FBBF24','#F472B6','#818CF8'), blend='screen', op=0.92, shadow=0.55, rim=0.55)

def head(t):
    return [f'<svg xmlns="http://www.w3.org/2000/svg" width="1024" height="1024" viewBox="0 0 1024 1024">',
    f'''<defs>
    <linearGradient id="body" x1="0" y1="0" x2="0.3" y2="1">{t['body']}</linearGradient>
    <radialGradient id="vig" cx="0.5" cy="0.45" r="0.75">
      <stop offset="0.6" stop-color="{t['vign']}" stop-opacity="0"/><stop offset="1" stop-color="{t['vign']}" stop-opacity="{t['vop']}"/>
    </radialGradient>
    <linearGradient id="rim" x1="0" y1="0" x2="0" y2="1">
      <stop offset="0" stop-color="#FFFFFF" stop-opacity="{t['rim']}"/><stop offset="0.6" stop-color="#FFFFFF" stop-opacity="0"/>
    </linearGradient>
    <filter id="sh" x="-60%" y="-60%" width="220%" height="240%">
      <feGaussianBlur in="SourceAlpha" stdDeviation="26"/><feOffset dy="22"/>
      <feComponentTransfer><feFuncA type="linear" slope="{t['shadow']}"/></feComponentTransfer>
      <feMerge><feMergeNode/><feMergeNode in="SourceGraphic"/></feMerge>
    </filter>
    <clipPath id="body"><path d="{P}"/></clipPath>
  </defs>''',
    f'<path d="{P}" fill="url(#body)"/>', f'<path d="{P}" fill="url(#vig)"/>']

def pills_kv(name, t, angle=-10, pw=580, ph=176, spread=118, key_w=0.24, gap=16):
    """A 键值片：每片被一道细缝切成「键 | 值」两段，读成数据行。"""
    cx, cy = 512, 512
    s = head(t)
    s.append(f'<g clip-path="url(#body)" transform="rotate({angle} {cx} {cy})">')
    ys=[cy-spread, cy, cy+spread]; xs=[cx-44, cx, cx+44]
    s.append('<g filter="url(#sh)">')
    for x0,y0,c in zip(xs,ys,t['colors']):
        L = x0-pw/2; kw = pw*key_w
        # 键段（左，短）+ 值段（右，长），中间留缝
        s.append(f'<rect x="{L}" y="{y0-ph/2}" width="{kw-gap/2}" height="{ph}" rx="{ph/2}" fill="{c}" fill-opacity="{t["op"]}" style="mix-blend-mode:{t["blend"]}"/>')
        s.append(f'<rect x="{L+kw+gap/2}" y="{y0-ph/2}" width="{pw-kw-gap/2}" height="{ph}" rx="{ph/2}" fill="{c}" fill-opacity="{t["op"]}" style="mix-blend-mode:{t["blend"]}"/>')
    s.append('</g>')
    for x0,y0,c in zip(xs,ys,t['colors']):
        L = x0-pw/2; kw = pw*key_w
        s.append(f'<rect x="{L+6}" y="{y0-ph/2+4}" width="{kw-gap/2-12}" height="{ph*0.55}" rx="{ph/2}" fill="url(#rim)"/>')
        s.append(f'<rect x="{L+kw+gap/2+6}" y="{y0-ph/2+4}" width="{pw-kw-gap/2-12}" height="{ph*0.55}" rx="{ph/2}" fill="url(#rim)"/>')
    s.append('</g></svg>')
    open(f'/tmp/icons/{name}.svg','w').write('\n'.join(s))

def discs(name, t, w=520, h=150, ry=52, gap=-30):
    """B 玻璃圆盘：三个半透明的圆柱切片——数据库最直白的形，用材质救回来。"""
    cx = 512
    total = 3*h + 2*gap
    top = 512 - total/2 + 20
    s = head(t)
    s.append('<g clip-path="url(#body)">')
    s.append('<g filter="url(#sh)">')
    # 从下往上画，让上面的透过来
    for i, c in reversed(list(enumerate(t['colors']))):
        y0 = top + i*(h+gap)
        L, R = cx-w/2, cx+w/2
        # 圆柱侧面：上下各半个椭圆
        d = f'M{L} {y0} A{w/2} {ry} 0 0 0 {R} {y0} V{y0+h} A{w/2} {ry} 0 0 1 {L} {y0+h} Z'
        s.append(f'<path d="{d}" fill="{c}" fill-opacity="{t["op"]}" style="mix-blend-mode:{t["blend"]}"/>')
        # 顶面：更亮的椭圆
        s.append(f'<ellipse cx="{cx}" cy="{y0}" rx="{w/2}" ry="{ry}" fill="{c}" fill-opacity="{t["op"]}" style="mix-blend-mode:{t["blend"]}"/>')
        s.append(f'<ellipse cx="{cx}" cy="{y0}" rx="{w/2-6}" ry="{ry-5}" fill="#FFFFFF" fill-opacity="{0.42 if t is DAY else 0.30}"/>')
    s.append('</g>')
    # 顶面高光弧
    for i, c in enumerate(t['colors']):
        y0 = top + i*(h+gap)
        s.append(f'<ellipse cx="{cx}" cy="{y0-2}" rx="{w/2-10}" ry="{ry-8}" fill="none" stroke="#FFFFFF" stroke-opacity="0.55" stroke-width="5"/>')
    s.append('</g></svg>')
    open(f'/tmp/icons/{name}.svg','w').write('\n'.join(s))

pills_kv('kv-day', DAY); pills_kv('kv-night', NIGHT)
discs('disc-day', DAY);  discs('disc-night', NIGHT)
print('ok')
