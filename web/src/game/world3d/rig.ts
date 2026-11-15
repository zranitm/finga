// Which joints an action's clip drives on its own while the legs walk: the KayKit rig splits cleanly at its spine.

export interface Joint {
  readonly name: string;
  readonly parent: string | null;
}

/** Arm IK controls hang off the root, not the spine, but move with the arms. */
const ARM_CONTROLS = ['elbowIK.', 'handIK.'];

/** The upper body: from spine up, with the arms' controls; every other joint is the lower body's. */
export function upperBody(joints: readonly Joint[], spine = 'spine'): ReadonlySet<string> {
  const parent = new Map(joints.map((j) => [j.name, j.parent]));
  const under = (name: string): boolean => {
    for (let at: string | null | undefined = name; at; at = parent.get(at)) {
      if (at === spine) {
        return true;
      }
    }
    return false;
  };
  return new Set(
    joints
      .filter((j) => under(j.name) || ARM_CONTROLS.some((c) => j.name.startsWith(c)))
      .map((j) => j.name),
  );
}
