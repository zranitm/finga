# The sea's props, our own work (underground-and-underwater.md section 9): coral, kelp, sea grass, shells and a fish, low-poly in flat colours like the KayKit and Quaternius packs.
# Run headless: Blender --background --python scripts/sea_props.py -- <out dir>; each prop is written as <name>.glb, its base on the ground at the origin, facing +z.
# Every shape comes from fixed numbers and a seeded random, so a run makes the same files.
import math
import random
import sys

import bmesh
import bpy
from mathutils import Euler, Matrix, Vector

OUT = sys.argv[sys.argv.index("--") + 1]

# Flat colours, as linear sRGB the exporter writes as baseColorFactor.
def srgb(hex_):
    c = [int(hex_[i : i + 2], 16) / 255 for i in (1, 3, 5)]
    return tuple(x / 12.92 if x <= 0.04045 else ((x + 0.055) / 1.055) ** 2.4 for x in c) + (1.0,)


def clear():
    bpy.ops.wm.read_factory_settings(use_empty=True)


def material(name, hex_):
    m = bpy.data.materials.get(name) or bpy.data.materials.new(name)
    m.use_nodes = True
    bsdf = m.node_tree.nodes["Principled BSDF"]
    bsdf.inputs["Base Color"].default_value = srgb(hex_)
    bsdf.inputs["Roughness"].default_value = 1.0
    bsdf.inputs["Metallic"].default_value = 0.0
    return m


class Mesh:
    """One object's faces, each in one of its materials, flat shaded."""

    def __init__(self, name):
        self.name = name
        self.bm = bmesh.new()
        self.mats = []

    def slot(self, mat):
        if mat not in self.mats:
            self.mats.append(mat)
        return self.mats.index(mat)

    def add(self, geom_faces, mat):
        k = self.slot(mat)
        for f in geom_faces:
            f.material_index = k
            f.smooth = False

    def prism(self, a, b, r0, r1, mat, sides=5, twist=0.0):
        """A tapered prism from a to b, its ends r0 and r1 across; r1 0 makes a cone."""
        a, b = Vector(a), Vector(b)
        axis = b - a
        rot = axis.to_track_quat("Z", "Y").to_matrix().to_4x4()
        ring = lambda r, at, off: [
            self.bm.verts.new(at + (rot @ Vector((r * math.cos(2 * math.pi * i / sides + off), r * math.sin(2 * math.pi * i / sides + off), 0))).to_3d())
            for i in range(sides)
        ]
        bottom = ring(r0, a, 0.0)
        faces = [self.bm.faces.new(list(reversed(bottom)))]
        if r1 <= 0:
            tip = self.bm.verts.new(b)
            faces += [self.bm.faces.new((bottom[i], bottom[(i + 1) % sides], tip)) for i in range(sides)]
        else:
            top = ring(r1, b, twist)
            faces += [self.bm.faces.new((bottom[i], bottom[(i + 1) % sides], top[(i + 1) % sides], top[i])) for i in range(sides)]
            faces.append(self.bm.faces.new(top))
        self.add(faces, mat)

    def blob(self, at, size, mat, subdiv=1, squash=(1, 1, 1), rng=None, jitter=0.0):
        """A low ico sphere at at, scaled by size and squash, its points pushed about by jitter."""
        g = bmesh.ops.create_icosphere(self.bm, subdivisions=subdiv, radius=size)
        for v in g["verts"]:
            v.co = Vector((v.co.x * squash[0], v.co.y * squash[1], v.co.z * squash[2]))
            if rng and jitter:
                v.co += Vector((rng.uniform(-1, 1), rng.uniform(-1, 1), rng.uniform(-1, 1))) * jitter * size
            v.co += Vector(at)
        self.add({f for v in g["verts"] for f in v.link_faces}, mat)
        return g["verts"]

    def scale(self, k):
        """Grows every point about the origin, its base staying on the ground."""
        for v in self.bm.verts:
            v.co *= k

    def quad(self, pts, mat):
        self.add([self.bm.faces.new([self.bm.verts.new(Vector(p)) for p in pts])], mat)

    def done(self):
        me = bpy.data.meshes.new(self.name)
        self.bm.normal_update()
        self.bm.to_mesh(me)
        self.bm.free()
        for m in self.mats:
            me.materials.append(m)
        ob = bpy.data.objects.new(self.name, me)
        bpy.context.scene.collection.objects.link(ob)
        return ob


def export(name):
    for ob in bpy.context.scene.objects:
        ob.select_set(True)
    tris = sum(len(p.vertices) - 2 for ob in bpy.context.scene.objects if ob.type == "MESH" for p in ob.data.polygons)
    bpy.ops.export_scene.gltf(filepath=f"{OUT}/{name}.glb", export_format="GLB", use_selection=True, export_apply=True, export_yup=True, export_normals=True, export_texcoords=False, export_materials="EXPORT")
    print(f"{name}: {tris} triangles")


# Facing: the exporter turns Blender's -y into glTF's +z, so a front looks along -y here.


def coral_branch():
    """Branching coral, about 0.9 m: a trunk splitting twice, each tip a bead."""
    rng = random.Random(1)
    body, tips = material("Main", "#f07a5a"), material("Tips", "#ffc9a8")
    m = Mesh("coral_branch")

    def grow(a, d, length, r, depth):
        b = a + d * length
        m.prism(a, b, r, r * 0.72, body, sides=5)
        if depth == 0:
            m.blob(b, r * 1.35, tips, subdiv=1)
            return
        for k in range(2 if depth > 1 else rng.choice((2, 3))):
            spin = rng.uniform(0, 2 * math.pi) + k * math.pi
            tilt = rng.uniform(0.35, 0.65)
            nd = (Euler((tilt * math.cos(spin), tilt * math.sin(spin), 0)).to_matrix() @ d).normalized()
            grow(b, nd, length * rng.uniform(0.7, 0.85), r * 0.72, depth - 1)

    for k, spin in enumerate((0.3, 2.4, 4.4)):
        d = (Euler((0.3 * math.cos(spin), 0.3 * math.sin(spin), 0)).to_matrix() @ Vector((0, 0, 1))).normalized()
        grow(Vector((0.06 * math.cos(spin), 0.06 * math.sin(spin), 0)), d, 0.32 - 0.04 * k, 0.07, 2)
    m.done()


def coral_fan():
    """A sea fan, about 1 m tall and as wide: ribs fanning out in one upright plane, thin as a leaf."""
    rng = random.Random(2)
    body, base = material("Main", "#9a5fd6"), material("Base", "#6a3fa0")
    m = Mesh("coral_fan")
    m.prism((0, 0, 0), (0, 0, 0.18), 0.07, 0.05, base, sides=5)
    for i in range(9):
        a = math.radians(-62 + i * 15.5 + rng.uniform(-3, 3))
        root = Vector((0, 0, 0.16))
        mid = root + Vector((math.sin(a) * 0.45, 0, math.cos(a) * 0.45))
        m.prism(root, mid, 0.028, 0.02, body, sides=4)
        for s in (-1, 1):
            b = a + s * math.radians(rng.uniform(8, 14))
            m.prism(mid, mid + Vector((math.sin(b) * 0.42, 0, math.cos(b) * 0.42)), 0.02, 0.008, body, sides=4)
    # a thin web between the ribs, both faces, so the fan reads as a sheet from any side
    for i in range(8):
        a0, a1 = math.radians(-60 + i * 15.5), math.radians(-60 + (i + 1) * 15.5)
        for y in (-0.006, 0.006):
            pts = [(0, y, 0.2), (math.sin(a0) * 0.8, y, 0.16 + math.cos(a0) * 0.8), (math.sin(a1) * 0.8, y, 0.16 + math.cos(a1) * 0.8)]
            m.quad(pts if y > 0 else list(reversed(pts)), material("Web", "#b884e6"))
    m.done()


def coral_brain():
    """Brain coral, a lumpy dome about 0.8 m across and 0.3 m high, its meandering grooves a darker flat colour."""
    rng = random.Random(3)
    m = Mesh("coral_brain")
    verts = m.blob((0, 0, 0), 0.4, material("Main", "#c9cf5e"), subdiv=3, squash=(1, 1, 0.75), rng=rng, jitter=0.05)
    under = [v for v in verts if v.co.z < -0.02]
    bmesh.ops.delete(m.bm, geom=under, context="VERTS")
    for v in m.bm.verts:
        v.co.z = max(v.co.z, 0.0)
    bmesh.ops.contextual_create(m.bm, geom=[e for e in m.bm.edges if e.is_boundary])
    groove = m.slot(material("Grooves", "#6c7a22"))
    for f in m.bm.faces:
        f.smooth = False
        c = f.calc_center_median()
        if c.z > 0.03 and math.sin(c.x * 24 + 2.4 * math.sin(c.y * 15)) > 0.2:
            f.material_index = groove
    m.done()


def kelp():
    """A kelp bed's plant, about 3 m: four stems swaying from their base, broad blades along them."""
    rng = random.Random(4)
    stem, blade = material("Stem", "#56702a"), material("Main", "#7d9a33")
    m = Mesh("kelp")
    for s in range(4):
        spin = s * math.pi / 2 + rng.uniform(-0.4, 0.4)
        h = rng.uniform(2.4, 3.2)
        prev = Vector((0.08 * math.cos(spin), 0.08 * math.sin(spin), 0))
        n = 7
        for k in range(1, n + 1):
            t = k / n
            p = Vector((prev.x + 0.06 * math.cos(spin) + 0.05 * math.sin(t * 5 + s), prev.y + 0.06 * math.sin(spin), h * t))
            m.prism(prev, p, 0.025, 0.02, stem, sides=4)
            if k % 2 == 1 or k == n:
                side = 1 if (k + s) % 2 else -1
                out = Vector((math.cos(spin + side * 1.2), math.sin(spin + side * 1.2), 0)) * 0.32
                tip = p + out + Vector((0, 0, 0.28))
                wide = Vector((-out.y, out.x, 0)).normalized() * 0.07
                for flip in (1, -1):
                    pts = [p, p + out * 0.5 + wide + Vector((0, 0, 0.1)), tip, p + out * 0.5 - wide + Vector((0, 0, 0.12))]
                    m.quad(pts if flip > 0 else list(reversed(pts)), blade)
            prev = p
    m.done()


def sea_grass():
    """A clump of sea grass, about 0.5 m: a dozen thin curved blades."""
    rng = random.Random(5)
    green = material("Main", "#5fae4e")
    m = Mesh("sea_grass")
    for i in range(12):
        a = rng.uniform(0, 2 * math.pi)
        r = rng.uniform(0, 0.12)
        base = Vector((r * math.cos(a), r * math.sin(a), 0))
        lean = Vector((math.cos(a), math.sin(a), 0)) * rng.uniform(0.08, 0.18)
        h = rng.uniform(0.35, 0.6)
        side = Vector((-math.sin(a), math.cos(a), 0)) * 0.025
        mid = base + lean * 0.4 + Vector((0, 0, h * 0.55))
        tip = base + lean + Vector((0, 0, h))
        for flip in (1, -1):
            pts = [base - side, base + side, mid + side * 0.6, tip, mid - side * 0.6]
            m.quad(pts if flip > 0 else list(reversed(pts)), green)
    m.done()


def shell_scallop():
    """A scallop shell, about 0.5 m across, big enough to read from the game's camera: a ribbed fan, domed, lying on the ground."""
    shell, rib = material("Main", "#f2c9a0"), material("Ribs", "#e08e6a")
    m = Mesh("shell_scallop")
    ribs = 9
    for i in range(ribs):
        a0 = math.radians(-70 + i * 140 / ribs)
        a1 = math.radians(-70 + (i + 1) * 140 / ribs)
        am = (a0 + a1) / 2
        hinge = Vector((0, 0.1, 0.01))
        p0 = Vector((math.sin(a0) * 0.15, 0.1 - math.cos(a0) * 0.22, 0.02))
        p1 = Vector((math.sin(a1) * 0.15, 0.1 - math.cos(a1) * 0.22, 0.02))
        crest = Vector((math.sin(am) * 0.15, 0.1 - math.cos(am) * 0.23, 0.05))
        top = Vector((math.sin(am) * 0.07, 0.1 - math.cos(am) * 0.1, 0.07))
        m.quad([hinge, p0, crest, top], shell if i % 2 else rib)
        m.quad([hinge, top, crest, p1], rib if i % 2 else shell)
    for s in (-1, 1):  # the ears by the hinge
        m.quad([(0, 0.1, 0.01), (s * 0.06, 0.13, 0.01), (s * 0.05, 0.1, 0.03)][:: s], shell)
    m.scale(1.6)
    m.done()


def shell_conch():
    """A conch shell, about 0.5 m long, lying on its side: a big body whorl, a short knobbed spire, and a flared pink lip along its open side."""
    shell, mouth = material("Main", "#f1c88e"), material("Mouth", "#ee7f8a")
    m = Mesh("shell_conch")
    # the body whorl, wide in its middle, then the spire narrowing to its point
    rings = [(-0.16, 0.04), (-0.1, 0.085), (-0.02, 0.1), (0.04, 0.075), (0.09, 0.045), (0.13, 0.025), (0.16, 0.0)]
    for (y0, r0), (y1, r1) in zip(rings, rings[1:]):
        m.prism((0, y0, 0.09), (0, y1, 0.09), r0, r1, shell, sides=6, twist=0.5)
    for y, r in rings[2:5]:  # the knobs round the shoulder
        for a in (0.6, 1.6, 2.6):
            at = Vector((math.cos(a) * r, y, 0.09 + math.sin(a) * r))
            m.prism(at, at + Vector((math.cos(a), 0, math.sin(a))) * 0.04, 0.015, 0, shell, sides=4)
    # the lip: a pink slit along the top of the body whorl, flaring out to one side
    m.blob((0.03, -0.07, 0.17), 0.05, mouth, subdiv=1, squash=(0.6, 1.8, 0.35))
    m.quad([(0.06, -0.15, 0.1), (0.17, -0.1, 0.08), (0.16, 0.01, 0.1), (0.07, 0.02, 0.15)], mouth)
    m.quad([(0.07, 0.02, 0.15), (0.16, 0.01, 0.1), (0.17, -0.1, 0.08), (0.06, -0.15, 0.1)], shell)
    m.scale(1.6)
    m.done()


def fish():
    """A small fish, 0.3 m nose to tail, blue with a yellow tail and fins, facing +z in glTF: one of a school."""
    body, fin = material("Main", "#3d7fe0"), material("Fins", "#ffd23f")
    m = Mesh("fish")
    m.blob((0, 0, 0), 0.07, body, subdiv=1, squash=(0.55, 1.9, 1.0))
    tail = Vector((0, 0.12, 0))  # behind it: Blender's +y is glTF's -z
    for flip in (1, -1):
        pts = [tail, Vector((0, 0.22, 0.07)), Vector((0, 0.2, 0)), Vector((0, 0.22, -0.07))]
        m.quad(pts if flip > 0 else list(reversed(pts)), fin)
        top = [Vector((0, -0.02, 0.06)), Vector((0, 0.07, 0.1)), Vector((0, 0.08, 0.05))]
        m.quad(top if flip > 0 else list(reversed(top)), fin)
    for s in (-1, 1):  # the eyes
        m.blob((s * 0.036, -0.08, 0.02), 0.012, material("Eye", "#1b1b2a"), subdiv=1)
    m.done()


for make in (coral_branch, coral_fan, coral_brain, kelp, sea_grass, shell_scallop, shell_conch, fish):
    clear()
    make()
    export(make.__name__)
