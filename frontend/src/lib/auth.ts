import { NextResponse } from 'next/server';
import { jwtVerify } from 'jose';

const jwtSecretValue = process.env.JWT_SECRET;
if (!jwtSecretValue) {
    console.error('[SECURITY] FATAL: JWT_SECRET environment variable is not set.');
}
const JWT_SECRET = new TextEncoder().encode(jwtSecretValue || '');

export interface AuthContext {
    userId: string;
    walletAddress: string;
    role: string;
}

export async function requireAuth(request: Request): Promise<{ auth?: AuthContext; error?: NextResponse }> {
    try {
        const authHeader = request.headers.get('Authorization');

        if (!authHeader) {
            return { error: NextResponse.json({ error: 'Akses Ditolak: Token tidak ditemukan' }, { status: 401 }) };
        }

        const parts = authHeader.split(' ');
        if (parts.length !== 2 || parts[0] !== 'Bearer') {
            return { error: NextResponse.json({ error: 'Akses Ditolak: Format token salah' }, { status: 401 }) };
        }

        const token = parts[1];

        // Verify the JWT
        const { payload } = await jwtVerify(token, JWT_SECRET);

        if (!payload || !payload.user_id) {
            return { error: NextResponse.json({ error: 'Sesi berakhir atau token tidak valid' }, { status: 403 }) };
        }

        return {
            auth: {
                userId: payload.user_id as string,
                walletAddress: payload.wallet_address as string,
                role: payload.role as string,
            }
        };

    } catch (error) {
        console.error('JWT Verification Error:', error);
        return { error: NextResponse.json({ error: 'Sesi berakhir atau token tidak valid' }, { status: 403 }) };
    }
}
