# The waypoint stone, our own work (large-regions.md section 8's Look): a low octagonal plinth, a tapered six-sided obelisk to 2.82 m, a cyan crystal floating over it to 3.55 m, and rune strips up three faces.
# Run headless: Blender --background --python scripts/waypoint_stone.py -- <out dir>; it writes <out dir>/waypoint_stone.glb, its base on the ground at the origin.
# The meshes are named for the client, which lights Crystal and Rune by whether the stone is found: Plinth, Stone, Crystal, Rune.
import math
import os
import sys

import bmesh
import bpy

OUT = sys.argv[sys.argv.index("--") + 1]
bpy.ops.wm.read_factory_settings(use_empty=True)


def linear(c):
    c = c / 255
    return c / 12.92 if c <= 0.04045 else ((c + 0.055) / 1.055) ** 2.4


def material(name, rgb):
    m = bpy.data.materials.new(name)
    try:
        m.use_nodes = True
    except AttributeError:
        pass
    nodes = m.node_tree.nodes
    bsdf = next((n for n in nodes if n.type == "BSDF_PRINCIPLED"), None)
    if bsdf is None:
        bsdf = nodes.new("ShaderNodeBsdfPrincipled")
        outp = next(n for n in nodes if n.type == "OUTPUT_MATERIAL")
        m.node_tree.links.new(bsdf.outputs[0], outp.inputs[0])
    bsdf.inputs["Base Color"].default_value = (*[linear(v) for v in rgb], 1)
    bsdf.inputs["Roughness"].default_value = 0.9
    return m


STONE = material("Stone", (0x6F, 0x6A, 0x63))
PLINTH = material("Plinth", (0x5E, 0x5A, 0x54))
CRYSTAL = material("Crystal", (0, 220, 230))  # the bake debug map's waypoint mark
RUNE = material("Rune", (0, 220, 230))


def obj(name, verts, faces, mat):
    me = bpy.data.meshes.new(name)
    me.from_pydata(verts, [], faces)
    bm = bmesh.new()
    bm.from_mesh(me)
    bmesh.ops.recalc_face_normals(bm, faces=bm.faces)
    bm.to_mesh(me)
    bm.free()
    me.update()
    o = bpy.data.objects.new(name, me)
    bpy.context.scene.collection.objects.link(o)
    me.materials.append(mat)
    return o


def rings(sides, levels, turn=0.0):
    """A closed prism through rings (radius, z), bottom to top, capped at both ends."""
    verts, faces = [], []
    for r, z in levels:
        for k in range(sides):
            a = 2 * math.pi * (k + turn) / sides
            verts.append((r * math.cos(a), r * math.sin(a), z))
    for i in range(len(levels) - 1):
        for k in range(sides):
            a, b = i * sides + k, i * sides + (k + 1) % sides
            faces.append((a, b, b + sides, a + sides))
    faces.append(tuple(reversed(range(sides))))
    top = (len(levels) - 1) * sides
    faces.append(tuple(range(top, top + sides)))
    return verts, faces


# The plinth sinks 0.3 m so a slope never shows its foot, and chamfers to its top at 0.18 m.
obj("Plinth", *rings(8, [(0.78, -0.3), (0.78, 0.1), (0.68, 0.18)], 0.5), PLINTH)

# The obelisk: radius 0.5 at the plinth (the 0.5 m solid), 0.30 at 2.70 m, chamfered to a 0.2 m top at 2.82 m.
R0, Z0, R1, Z1 = 0.5, 0.18, 0.30, 2.70
obj("Stone", *rings(6, [(R0, Z0 - 0.05), (R0, Z0), (R1, Z1), (0.2, 2.82)]), STONE)

# The crystal: a six-sided bipyramid, from 2.92 m to 3.55 m, widest at 3.18 m, turned half a face from the obelisk.
cv = [(0, 0, 2.92)] + [(0.2 * math.cos(2 * math.pi * (k + 0.5) / 6), 0.2 * math.sin(2 * math.pi * (k + 0.5) / 6), 3.18) for k in range(6)] + [(0, 0, 3.55)]
cf = [(0, 1 + (k + 1) % 6, 1 + k) for k in range(6)] + [(7, 1 + k, 1 + (k + 1) % 6) for k in range(6)]
obj("Crystal", cv, cf, CRYSTAL)


# Runes: a strip up faces 0, 2 and 4 with a diamond over it, 12 mm proud of the face.
def radius(z):
    return R0 + (R1 - R0) * (z - Z0) / (Z1 - Z0)


def on_face(k, z, t):
    am = 2 * math.pi * (k + 0.5) / 6
    d = radius(z) * math.cos(math.pi / 6) + 0.012
    return (d * math.cos(am) - t * math.sin(am), d * math.sin(am) + t * math.cos(am), z)


rv, rf = [], []
for k in (0, 2, 4):
    quads = [
        [(0.55, -0.035), (0.55, 0.035), (2.2, 0.035), (2.2, -0.035)],
        [(2.3, 0.0), (2.42, 0.08), (2.54, 0.0), (2.42, -0.08)],
        [(1.05, -0.11), (1.05, 0.11), (1.11, 0.11), (1.11, -0.11)],
        [(1.65, -0.09), (1.65, 0.09), (1.71, 0.09), (1.71, -0.09)],
    ]
    for q in quads:
        base = len(rv)
        rv += [on_face(k, z, t) for z, t in q]
        rf.append((base, base + 1, base + 2, base + 3))
me = bpy.data.meshes.new("Rune")
me.from_pydata(rv, [], rf)
me.update()
o = bpy.data.objects.new("Rune", me)
bpy.context.scene.collection.objects.link(o)
me.materials.append(RUNE)
# Each strip faces out from the obelisk: flip any whose normal points in.
bm = bmesh.new()
bm.from_mesh(me)
for f in bm.faces:
    c = f.calc_center_median()
    if f.normal.x * c.x + f.normal.y * c.y < 0:
        f.normal_flip()
bm.to_mesh(me)
bm.free()

out = os.path.join(OUT, "waypoint_stone.glb")
os.makedirs(OUT, exist_ok=True)
bpy.ops.export_scene.gltf(filepath=out, export_format="GLB", export_apply=True, export_yup=True)
print("wrote", out)
