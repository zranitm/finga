import { create } from '@bufbuild/protobuf';
import { act, fireEvent, render, screen, within } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { pieces } from '@/progression/pieces';
import { ERR } from '@/protocol/errors';
import { CharacterViewSchema, ResponseSchema, WelcomeSchema } from '@/protocol/messages';
import { character } from '@/state/character';
import { OWN_LOOK, wardrobe } from '@/state/wardrobe';
import { refusal } from '@/testing/refusal';
import { withoutKeys } from '@/testing/requests';
import { resetStores } from '@/testing/reset-stores';
import { user } from '@/testing/user';

import { Wardrobe } from './Wardrobe';
import type { WardrobeProps } from './Wardrobe';

const ok = create(ResponseSchema, { ok: true });
const COLLECTED = ['knight_plate', 'knight_helm'];

/** The wardrobe open on a Warrior whose account holds COLLECTED; the server answers each request with answer. */
function open(answer: () => Promise<unknown> = () => Promise.resolve(ok)) {
  const figure = { show: vi.fn(), turn: vi.fn(), close: vi.fn() };
  const props: WardrobeProps = {
    socket: { request: vi.fn(answer) } as never,
    rest: {
      getWardrobe: vi.fn(() =>
        Promise.resolve({
          pieces: COLLECTED.map((id) => ({ id, cause: 'item', added_at: '2026-11-25T00:00:00Z' })),
        }),
      ),
    },
    figureIn: vi.fn(() => figure),
    onClose: vi.fn(),
  };
  const view = render(<Wardrobe {...props} />);
  return { figure, props, view };
}

const tiles = () =>
  [...screen.getByRole('tabpanel').querySelectorAll('button')].map((b) => [
    b.textContent,
    b.classList.contains('missing') ? 'greyed' : 'lit',
  ]);
const COUNTED = `Collected 2 of ${pieces.length}`; // the two the server answers, of the catalogue
const tab = (name: string) => user.click(screen.getByRole('tab', { name }));

describe('the wardrobe (outfits.md section 7)', () => {
  beforeEach(() => {
    resetStores();
    act(() => {
      character.getState().applyWelcome(
        create(WelcomeSchema, {
          token: 't',
          character: create(CharacterViewSchema, { id: 'c1', name: 'Ayla', class: 'warrior' }),
        }),
      );
    });
  });

  it('lists each slot’s pieces, the class’s own look first, headwear’s Hidden and wings’ None: collected ones lit, the rest greyed, and a weapon look of the class’s kind alone', async () => {
    open();
    expect(screen.getByText('Loading your wardrobe…')).toBeInTheDocument();
    await screen.findByText(COUNTED);
    expect(tiles()).toEqual([
      ['Class look', 'lit'],
      ["Knight's Plate", 'lit'],
      ['Barbarian Furs', 'greyed'],
      ["Rogue's Leathers", 'greyed'],
      ["Mage's Robe", 'greyed'],
      ['Crimson Plate', 'greyed'],
      ['Platinum Plate', 'greyed'],
      ["Veteran's Plate", 'greyed'],
    ]);
    await tab('Headwear');
    expect(tiles().map(([n]) => n)).toEqual([
      'Class look',
      'Hidden',
      "Knight's Helm",
      'Bear Hood',
      'Pointed Hat',
      'Silver Visor',
      'Gold Visor',
      "Wanderer's Hat",
      'Crimson Helm',
      "Victor's Horns",
    ]);
    await tab('Weapon');
    expect(tiles()).toEqual([
      ['Class look', 'lit'],
      ['Greatsword', 'greyed'],
      ['Greataxe', 'greyed'],
      ['Bronze Sword', 'greyed'],
    ]); // no wand, knife or crossbow for a Warrior
    await tab('Wings');
    expect(tiles()).toEqual([['None', 'lit']]);
  });

  it('tries anything on, the figure dressed as picked; Wear sends the collected picks alone, and Reset goes back to what’s worn', async () => {
    const { figure, props } = open();
    await screen.findByText(COUNTED);
    expect(figure.show).toHaveBeenLastCalledWith('warrior', OWN_LOOK);
    const wear = screen.getByRole('button', { name: 'Wear' });
    const reset = screen.getByRole('button', { name: 'Reset' });
    expect([wear, reset].map((b) => (b as HTMLButtonElement).disabled)).toEqual([true, true]);
    await user.click(screen.getByRole('button', { name: 'Barbarian Furs' }));
    expect(figure.show).toHaveBeenLastCalledWith('warrior', {
      ...OWN_LOOK,
      outfit: 'barbarian_furs',
    });
    expect([wear, reset].map((b) => (b as HTMLButtonElement).disabled)).toEqual([true, false]); // nothing collected to send
    const note = 'Pieces you haven’t collected stay as worn.';
    expect(screen.getByText(note)).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: "Knight's Plate" }));
    await tab('Headwear');
    await user.click(screen.getByRole('button', { name: 'Hidden' }));
    await tab('Weapon');
    await user.click(screen.getByRole('button', { name: 'Greatsword' }));
    expect(figure.show).toHaveBeenLastCalledWith('warrior', {
      ...OWN_LOOK,
      outfit: 'knight_plate',
      hideHeadwear: true,
      weapon: 'greatsword',
    });
    await user.click(wear);
    const sent = vi.mocked(props.socket.request).mock.calls.map(([t, d]) => ({
      t,
      d: d,
    }));
    expect(withoutKeys(sent)).toEqual([
      {
        t: 'setLook',
        d: { look: { ...OWN_LOOK, outfit: 'knight_plate', hideHeadwear: true } }, // the greatsword isn't collected: the class's own still
      },
    ]);
    act(() => {
      wardrobe.getState().wear({ ...OWN_LOOK, outfit: 'knight_plate', hideHeadwear: true }); // the player's next frame
    });
    await user.click(reset);
    expect(figure.show).toHaveBeenLastCalledWith('warrior', {
      ...OWN_LOOK,
      outfit: 'knight_plate',
      hideHeadwear: true,
    });
    expect([wear, reset].map((b) => (b as HTMLButtonElement).disabled)).toEqual([true, true]);
    expect(screen.queryByText(note)).toBeNull();
  });

  it('shows the server’s refusal', async () => {
    open(() => Promise.reject(refusal(ERR.IN_COMBAT)));
    await screen.findByText(COUNTED);
    await user.click(screen.getByRole('button', { name: "Knight's Plate" }));
    await user.click(screen.getByRole('button', { name: 'Wear' }));
    expect(await screen.findByRole('alert')).toHaveTextContent(refusal(ERR.IN_COMBAT).message);
  });

  it('says a wardrobe that won’t load', async () => {
    const figure = { show: vi.fn(), turn: vi.fn(), close: vi.fn() };
    render(
      <Wardrobe
        socket={{ request: vi.fn() } as never}
        rest={{ getWardrobe: () => Promise.reject(new Error('down')) }}
        figureIn={() => figure}
        onClose={vi.fn()}
      />,
    );
    expect(await screen.findByRole('alert')).toHaveTextContent('Your wardrobe didn’t load');
  });

  it('cards a piece on hover with its stars, slot, kind and how to get it', async () => {
    open();
    await screen.findByText(COUNTED);
    await tab('Weapon');
    fireEvent.mouseEnter(screen.getByRole('button', { name: 'Greatsword' }), {
      clientX: 40,
      clientY: 40,
    });
    const card = document.querySelector('.piece-card')!;
    expect(within(card as HTMLElement).getByText('Greatsword')).toBeInTheDocument();
    expect(card).toHaveTextContent('★★★');
    expect(card).toHaveTextContent('Weapon · one-handed');
    expect(card).toHaveTextContent('Not collected');
    expect(card).toHaveTextContent('Dropped by Cliff Reaper');
    fireEvent.mouseLeave(screen.getByRole('button', { name: 'Greatsword' }));
    expect(document.querySelector('.piece-card')).toBeNull();
  });

  it('draws its figure in its canvas, and lets it go as the window closes, which stops its drawing', async () => {
    const { figure, props, view } = open();
    await screen.findByText(COUNTED);
    expect(props.figureIn).toHaveBeenCalledWith(
      screen.getByLabelText('Your character, dressed as picked'),
    );
    expect(figure.close).not.toHaveBeenCalled();
    view.unmount();
    expect(figure.close).toHaveBeenCalledOnce();
  });

  it('opens with a piece the inventory’s Preview handed it tried on, at its tab', async () => {
    act(() => wardrobe.getState().preview('pointed_hat'));
    const { figure } = open();
    expect(wardrobe.getState().trying).toBeNull(); // taken as it opened, so the list coming in puts nothing back
    await screen.findByText(COUNTED);
    expect(screen.getByRole('tab', { name: 'Headwear' })).toHaveAttribute('aria-selected', 'true');
    expect(figure.show).toHaveBeenLastCalledWith('warrior', {
      ...OWN_LOOK,
      headwear: 'pointed_hat',
    });
    expect(screen.getByRole('button', { name: 'Pointed Hat' })).toHaveAttribute(
      'aria-pressed',
      'true',
    );
    expect(wardrobe.getState().trying).toBeNull(); // taken
  });

  it('tries on a piece handed while it’s open, over what’s tried on already', async () => {
    const { figure } = open();
    await screen.findByText(COUNTED);
    await user.click(screen.getByRole('button', { name: "Knight's Plate" }));
    act(() => wardrobe.getState().preview('greataxe'));
    expect(screen.getByRole('tab', { name: 'Weapon' })).toHaveAttribute('aria-selected', 'true');
    expect(figure.show).toHaveBeenLastCalledWith('warrior', {
      ...OWN_LOOK,
      outfit: 'knight_plate',
      weapon: 'greataxe',
    });
    expect(wardrobe.getState().trying).toBeNull();
  });
});
