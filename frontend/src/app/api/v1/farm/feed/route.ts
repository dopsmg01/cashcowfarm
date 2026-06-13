import { NextResponse } from 'next/server';
import { requireAuth } from '@/lib/auth';
import { supabaseAdmin } from '@/lib/supabase';

export async function POST(request: Request) {
    try {
        const { auth, error: authError } = await requireAuth(request);
        if (authError) return authError;
        if (!auth) throw new Error('Auth context missing');

        const { cow_id } = await request.json();

        if (!cow_id) {
            return NextResponse.json({ status: 'error', message: 'Cow ID is required' }, { status: 400 });
        }

        // 1. Fetch Inventory & Cow
        const { data: inventory, error: invError } = await supabaseAdmin
            .from('inventories')
            .select('*')
            .eq('user_id', auth.userId)
            .single();

        if (invError) throw invError;
        if (inventory.grass < 1) {
            return NextResponse.json({ status: 'error', message: 'Grass tidak cukup' }, { status: 422 });
        }

        const { data: cow, error: cowError } = await supabaseAdmin
            .from('cows')
            .select('*')
            .eq('id', cow_id)
            .eq('owner_id', auth.userId)
            .single();

        if (cowError) {
            return NextResponse.json({ status: 'error', message: 'Sapi tidak ditemukan atau bukan milik Anda' }, { status: 422 });
        }

        if (cow.happiness >= 100) {
            return NextResponse.json({ status: 'error', message: 'Sapi sudah kenyang (Happiness 100)' }, { status: 422 });
        }

        // 2. Perform updates (In a real production app, use Supabase RPC for atomic transactions)
        // Update Inventory
        const { error: upInvError } = await supabaseAdmin
            .from('inventories')
            .update({ grass: inventory.grass - 1 })
            .eq('id', inventory.id);

        if (upInvError) throw upInvError;

        // Update Cow
        const { error: upCowError } = await supabaseAdmin
            .from('cows')
            .update({
                happiness: Math.min(cow.happiness + 20, 100),
                last_fed_at: new Date().toISOString()
            })
            .eq('id', cow.id);

        if (upCowError) throw upCowError;

        return NextResponse.json({ status: 'success', message: 'Sapi berhasil diberi makan!' }, { status: 200 });

    } catch (error: any) {
        console.error('Feed error:', error);
        return NextResponse.json(
            { status: 'error', message: 'Internal server error' },
            { status: 500 }
        );
    }
}
