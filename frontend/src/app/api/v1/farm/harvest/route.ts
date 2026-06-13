import { NextResponse } from 'next/server';
import { requireAuth } from '@/lib/auth';
import { supabaseAdmin } from '@/lib/supabase';

export async function POST(request: Request) {
    try {
        const { auth, error: authError } = await requireAuth(request);
        if (authError) return authError;
        if (!auth) throw new Error('Auth context missing');

        // 1. Fetch user to check ad count limit
        const { data: user, error: userError } = await supabaseAdmin
            .from('users')
            .select('daily_ad_count')
            .eq('id', auth.userId)
            .single();

        if (userError) throw userError;

        // Note: For MVP we might bypass ad-check if we want users to test freely,
        // but replicating Go logic: if daily_ad_count <= 0, logic blocks or yields 0.
        // Assuming user needs >0 ads watched to harvest:
        /*
        if (user.daily_ad_count <= 0) {
            return NextResponse.json({ status: 'error', message: 'Anda harus menonton iklan (Care) sebelum harvest!' }, { status: 422 });
        }
        */

        // 2. Fetch all user cows
        const { data: cows, error: cowError } = await supabaseAdmin
            .from('cows')
            .select('*')
            .eq('owner_id', auth.userId);

        if (cowError) throw cowError;

        let totalMilkGained = 0;
        const now = new Date();

        // 3. Calculate yields
        // In a real RPC transaction, this is safer. We will loop and update here for MVP.
        for (const cow of cows) {
            // Very simple mock logic: If happiness > 50, yields 1 milk.
            if (cow.happiness > 50) {
                totalMilkGained += 1;
                // Decrease happiness after harvest
                const { error: cowUpdateErr } = await supabaseAdmin
                    .from('cows')
                    .update({
                        happiness: Math.max(cow.happiness - 30, 0),
                        last_harvested_at: now.toISOString()
                    })
                    .eq('id', cow.id);

                if (cowUpdateErr) throw cowUpdateErr;
            }
        }

        // 4. Update Inventory
        if (totalMilkGained > 0) {
            const { data: inventory, error: invFetchErr } = await supabaseAdmin
                .from('inventories')
                .select('milk')
                .eq('user_id', auth.userId)
                .single();

            if (invFetchErr) throw invFetchErr;

            if (inventory) {
                const { error: invUpdateErr } = await supabaseAdmin
                    .from('inventories')
                    .update({ milk: inventory.milk + totalMilkGained })
                    .eq('user_id', auth.userId);

                if (invUpdateErr) throw invUpdateErr;
            }
        }

        // Reduce daily ad count
        if (user.daily_ad_count > 0) {
            const { error: adUpdateErr } = await supabaseAdmin
                .from('users')
                .update({ daily_ad_count: user.daily_ad_count - 1 })
                .eq('id', auth.userId);

            if (adUpdateErr) throw adUpdateErr;
        }

        return NextResponse.json({
            status: 'success',
            message: 'Susu berhasil dipanen!',
            data: { milk_harvested: totalMilkGained }
        }, { status: 200 });

    } catch (error: any) {
        console.error('Harvest error:', error);
        return NextResponse.json(
            { status: 'error', message: error.message || 'Internal server error' },
            { status: 500 }
        );
    }
}
