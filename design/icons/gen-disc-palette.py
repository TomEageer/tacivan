# -*- coding: utf-8 -*-
"""降噪：中间交叠区颜色太多。四个方向各动一个变量，看哪个最干净。"""
exec(open('gen10.py').read().split("# T1：")[0])   # 复用 discs_tense / DAY / NIGHT

def variant(name, base, **kw):
    t = dict(base); t.update(kw); return t

# V1 邻近色：琥珀→珊瑚→玫红。三片本来就是一家人，叠出来的还是一家人
V1 = variant('v1', DAY, colors=('#F59E0B','#F0645A','#E0409A'))
# V2 去 multiply：正常叠放 + 稍高不透明度。交叠处不再"相乘"成深紫，只是轻微透出下层
V2 = variant('v2', DAY, blend='normal', op=0.90)
# V3 两个色相：琥珀与靛，中间片就是二者之间的那一档。少一个源头色，交叠自然少一半
V3 = variant('v3', DAY, colors=('#F59E0B','#C0559A','#6366F1'), blend='normal', op=0.92)
# V4 单一色相走明度：一整套珊瑚，三档深浅，靠明暗分层而不是靠颜色分层
V4 = variant('v4', DAY, colors=('#FFB36B','#F4744F','#D9463C'), blend='normal', op=0.94)

for n, t in [('n1', V1), ('n2', V2), ('n3', V3), ('n4', V4)]:
    discs_tense(n, t, dx=2, dy=-46)
print('ok')
