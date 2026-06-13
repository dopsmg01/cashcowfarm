import { NextResponse } from 'next/server';
import fs from 'fs';
import path from 'path';

export async function GET() {
    try {
        const readmePath = path.join(process.cwd(), '..', 'README.md');
        console.log('Loading whitepaper from:', readmePath);

        if (!fs.existsSync(readmePath)) {
            console.error('README.md not found at:', readmePath);
            return NextResponse.json({ error: 'File not found' }, { status: 404 });
        }

        const content = fs.readFileSync(readmePath, 'utf8');
        return NextResponse.json({ content });
    } catch (error) {
        console.error('Failed to read whitepaper:', error);
        return NextResponse.json({ error: 'Failed to read whitepaper' }, { status: 500 });
    }
}
