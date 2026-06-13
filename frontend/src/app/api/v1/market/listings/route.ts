import { NextResponse } from 'next/server';
import { requireAuth } from '@/lib/auth';
import { supabaseAdmin } from '@/lib/supabase';

export async function GET(request: Request) {
    try {
        const { auth, error: authError } = await requireAuth(request);
        if (authError) return authError;
        if (!auth) throw new Error('Auth context missing');

        const { data: listings, error } = await supabaseAdmin
            .from('market_listings')
            .select('*')
            .eq('status', 'OPEN');

        if (error) throw error;

        return NextResponse.json({
            status: 'success',
            message: 'Listings berhasil diambil',
            data: listings || []
        }, { status: 200 });

    } catch (error: any) {
        console.error('Listings error:', error);
        return NextResponse.json(
            { status: 'error', message: 'Failed to fetch market listings' },
            { status: 500 }
        );
    }
}
