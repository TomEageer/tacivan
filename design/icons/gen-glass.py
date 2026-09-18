# -*- coding: utf-8 -*-
"""
「叠层」：三片半透明的圆角色片，青 → 蓝 → 靛，错位叠放。
交叠处颜色自然加深——色彩是"混"出来的，不是画上去的。
每片顶缘一道细白高光，像磨砂玻璃的边。
"""
import math
CANVAS, BODY, N = 1024, 824, 5.0
def squircle(cx, cy, r, n=N, steps=900):
    pts=[]
    for i in range(steps):
        th=2*math.pi*i/steps; ct,st=math.cos(th),math.sin(th)
        pts.append((cx+r*math.copysign(abs(ct)**(2/n),ct), cy+r*math.copysign(abs(st)**(2/n),st)))
    return 'M %.2f %.2f '%pts[0]+' '.join('L %.2f %.2f'%p for p in pts[1:])+' Z'
P = squircle(CANVAS/2, CANVAS/2, BODY/2)

def build(name, light=True, angle=-10, blend='multiply', colors=('#22D3EE','#3B82F6','#6366F1'), spread=118, pw=560, ph=176, opacity=0.86):
    if light:
        body = '<stop offset="0" stop-color="#FFFFFF"/><stop offset="1" stop-color="#E9EFF7"/>'
        vign = '#0B1220'; vop = 0.10
    else:
        body = '<stop offset="0" stop-color="#1B2436"/><stop offset="1" stop-color="#0B1120"/>'
        vign = '#000000'; vop = 0.35
    cx, cy = 512, 512
    s=[f'<svg xmlns="http://www.w3.org/2000/svg" width="{CANVAS}" height="{CANVAS}" viewBox="0 0 {CANVAS} {CANVAS}">',
    f'''<defs>
    <linearGradient id="body" x1="0" y1="0" x2="0.3" y2="1">{body}</linearGradient>
    <radialGradient id="vig" cx="0.5" cy="0.45" r="0.75">
      <stop offset="0.6" stop-color="{vign}" stop-opacity="0"/><stop offset="1" stop-color="{vign}" stop-opacity="{vop}"/>
    </radialGradient>
    <linearGradient id="rim" x1="0" y1="0" x2="0" y2="1">
      <stop offset="0" stop-color="#FFFFFF" stop-opacity="0.75"/><stop offset="0.5" stop-color="#FFFFFF" stop-opacity="0"/>
    </linearGradient>
    <filter id="sh" x="-30%" y="-30%" width="160%" height="170%">
      <feGaussianBlur in="SourceAlpha" stdDeviation="26"/><feOffset dy="30" result="o"/>
      <feComponentTransfer><feFuncA type="linear" slope="{0.18 if light else 0.45}"/></feComponentTransfer>
      <feMerge><feMergeNode/><feMergeNode in="SourceGraphic"/></feMerge>
    </filter>
    <clipPath id="body"><path d="{P}"/></clipPath>
  </defs>''',
    f'<path d="{P}" fill="url(#body)"/>',
    f'<path d="{P}" fill="url(#vig)"/>',
    f'<g clip-path="url(#body)" transform="rotate({angle} {cx} {cy})">']
    # 三片：整体投影一次，色片用混合模式
    ys = [cy - spread, cy, cy + spread]
    xs = [cx - 44, cx, cx + 44]
    s.append('<g filter="url(#sh)">')
    for (x0,y0,c) in zip(xs,ys,colors):
        s.append(f'<rect x="{x0-pw/2}" y="{y0-ph/2}" width="{pw}" height="{ph}" rx="{ph/2}" fill="{c}" fill-opacity="{opacity}" style="mix-blend-mode:{blend}"/>')
    s.append('</g>')
    # 玻璃边：每片顶缘一道高光
    for (x0,y0,c) in zip(xs,ys,colors):
        s.append(f'<rect x="{x0-pw/2+6}" y="{y0-ph/2+4}" width="{pw-12}" height="{ph*0.55}" rx="{ph/2}" fill="url(#rim)" opacity="0.9"/>')
    s.append('</g></svg>')
    open(f'/tmp/icons/{name}.svg','w').write('\n'.join(s))

build('g-light', light=True)
build('g-light-flat', light=True, angle=0)
build('g-dark', light=False, blend='screen', opacity=0.92)
build('g-warm', light=True, colors=('#F59E0B','#EC4899','#6366F1'))
print('ok')
